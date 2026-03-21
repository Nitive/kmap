package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
	"unsafe"

	xkb "github.com/thegrumpylion/xkb-go"

	"keyboard/pkg/config"
	"keyboard/pkg/daemon"
)

const (
	defaultKeymapOutputPath   = "-"
	setupKeymapXKBKeycodeBase = 8
)

type capturedKey struct {
	Logical string `json:"logical"`
	Bytes   []byte `json:"bytes,omitempty"`
	Text    string `json:"text,omitempty"`
	Escaped string `json:"escaped,omitempty"`
	Skipped bool   `json:"skipped,omitempty"`
}

type layoutFile struct {
	SchemaVersion int           `json:"schema_version"`
	Layout        string        `json:"layout"`
	Keyboard      string        `json:"keyboard"`
	Device        string        `json:"device"`
	CapturedAt    string        `json:"captured_at"`
	Keys          []capturedKey `json:"keys"`
}

type promptReader interface {
	ReadLine(ctx context.Context) (string, error)
}

type sequenceReader interface {
	Next(ctx context.Context) ([]byte, error)
}

type terminalPromptReader struct {
	fd     int
	reader *bufio.Reader
}

type terminalSequenceReader struct {
	file *os.File
	fd   int
}

type termiosState struct {
	value syscall.Termios
}

var (
	setupKeymapInput                         = os.Stdin
	setupKeymapStdout              io.Writer = os.Stdout
	setupKeymapStderr              io.Writer = os.Stderr
	setupKeymapNowFn                         = time.Now
	setupKeymapPromptReaderFactory           = func(file *os.File) promptReader {
		return &terminalPromptReader{fd: int(file.Fd()), reader: bufio.NewReader(file)}
	}
	setupKeymapSequenceReaderFactory = func(file *os.File) sequenceReader {
		return &terminalSequenceReader{file: file, fd: int(file.Fd())}
	}
	setupKeymapPromptLabelsFn = buildPromptLabels
	setupKeymapSignalGrabFn   = daemon.SignalGrabOverride
)

var defaultLogicalOrder = []string{
	"grv", "1", "2", "3", "4", "5", "6", "7", "8", "9", "0", "-", "=", "bspc",
	"tab", "q", "w", "e", "r", "t", "y", "u", "i", "o", "p", "[", "]", "\\",
	"caps", "a", "s", "d", "f", "g", "h", "j", "k", "l", ";", "'", "ret",
	"lsft", "z", "x", "c", "v", "b", "n", "m", ",", ".", "/", "rsft",
	"lctl", "lmet", "lalt", "spc", "ralt", "rctl",
}

var logicalPromptBaseLabels = map[string]string{
	"grv":  "grave",
	"-":    "minus",
	"=":    "equal",
	"bspc": "backspace",
	"tab":  "tab",
	"[":    "left bracket",
	"]":    "right bracket",
	"\\":   "backslash",
	"caps": "caps lock",
	";":    "semicolon",
	"'":    "apostrophe",
	"ret":  "enter",
	"lsft": "left shift",
	",":    "comma",
	".":    "dot",
	"/":    "slash",
	"rsft": "right shift",
	"lctl": "left ctrl",
	"lmet": "left meta",
	"lalt": "left alt",
	"spc":  "space",
	"ralt": "right alt",
	"rctl": "right ctrl",
}

func runSetupKeymap(args []string) error {
	fs := flag.NewFlagSet("setup-keymap", flag.ContinueOnError)
	fs.SetOutput(setupKeymapStderr)

	devicePath := fs.String("device", "", "keyboard device path for metadata")
	outputPath := fs.String("output", defaultKeymapOutputPath, "output JSON path (`-` writes to stdout)")
	keyboardName := fs.String("name", "", "keyboard name for metadata (default: basename of --device)")
	layoutName := fs.String("layout", "", "layout label to store in output, e.g. us(dvorak) or ru")
	keysCSV := fs.String("keys", "", "comma-separated logical keys to capture in order (default: built-in order)")
	grab := fs.Bool("grab", true, "deprecated; ignored")
	signalDaemon := fs.Bool("signal-daemon", true, "temporarily ask the running daemon to release grabbed devices")

	fs.Usage = func() {
		_, _ = fmt.Fprintf(fs.Output(), "Usage: kmap setup-keymap --layout <name> [flags]\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}
	if !*grab {
		_, _ = fmt.Fprintln(setupKeymapStderr, "warning: --grab is deprecated and ignored")
	}
	if strings.TrimSpace(*layoutName) == "" {
		return errors.New("layout is required")
	}

	order := parseLogicalOrder(*keysCSV)
	if len(order) == 0 {
		return errors.New("no keys to capture (empty --keys)")
	}

	name := strings.TrimSpace(*keyboardName)
	if name == "" {
		trimmedDevice := strings.TrimSpace(*devicePath)
		if trimmedDevice == "" {
			name = "keyboard"
		} else {
			name = filepath.Base(trimmedDevice)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	promptLabels, err := setupKeymapPromptLabelsFn(ctx, order, strings.TrimSpace(*layoutName))
	if err != nil {
		return err
	}

	resumeDaemon, err := maybeReleaseDaemon(*signalDaemon)
	if err != nil {
		return err
	}
	defer resumeDaemon()

	keys, err := captureLayout(
		ctx,
		setupKeymapPromptReaderFactory(setupKeymapInput),
		setupKeymapSequenceReaderFactory(setupKeymapInput),
		setupKeymapStderr,
		order,
		promptLabels,
	)
	if err != nil {
		return err
	}

	layout := buildLayout(name, strings.TrimSpace(*devicePath), strings.TrimSpace(*layoutName), keys, setupKeymapNowFn())
	if err := writeLayoutJSON(*outputPath, layout, setupKeymapStdout); err != nil {
		return err
	}

	if strings.TrimSpace(*outputPath) == "" || strings.TrimSpace(*outputPath) == "-" {
		_, _ = fmt.Fprintf(setupKeymapStderr, "\nCaptured %d keys for %s\n", len(layout.Keys), layout.Layout)
	} else {
		_, _ = fmt.Fprintf(setupKeymapStderr, "\nSaved %d keys to %s\n", len(layout.Keys), *outputPath)
	}
	return nil
}

func maybeReleaseDaemon(enable bool) (func(), error) {
	if !enable {
		return func() {}, nil
	}

	err := setupKeymapSignalGrabFn(true)
	switch {
	case err == nil:
		time.Sleep(100 * time.Millisecond)
		return func() {
			if resumeErr := setupKeymapSignalGrabFn(false); resumeErr != nil && !errors.Is(resumeErr, daemon.ErrDaemonNotRunning) {
				_, _ = fmt.Fprintf(setupKeymapStderr, "warning: failed to restore daemon grabs: %v\n", resumeErr)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}, nil
	case errors.Is(err, daemon.ErrDaemonNotRunning):
		_, _ = fmt.Fprintln(setupKeymapStderr, "warning: daemon control pid not found; proceeding without releasing grabs")
		return func() {}, nil
	default:
		return nil, fmt.Errorf("release daemon grabs: %w", err)
	}
}

func parseLogicalOrder(keysCSV string) []string {
	if strings.TrimSpace(keysCSV) == "" {
		out := make([]string, len(defaultLogicalOrder))
		copy(out, defaultLogicalOrder)
		return out
	}

	parts := strings.Split(keysCSV, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		v := strings.TrimSpace(part)
		if v == "" {
			continue
		}
		out = append(out, v)
	}
	return out
}

func buildLayout(name string, device string, layout string, keys []capturedKey, now time.Time) layoutFile {
	return layoutFile{
		SchemaVersion: 2,
		Layout:        layout,
		Keyboard:      name,
		Device:        device,
		CapturedAt:    now.UTC().Format(time.RFC3339),
		Keys:          keys,
	}
}

func buildPromptLabels(ctx context.Context, logicalOrder []string, layoutName string) (map[string]string, error) {
	spec, err := parseSetupKeymapLayoutSpec(layoutName)
	if err != nil {
		return nil, err
	}

	xkbCtx := xkb.NewContext(ctx, xkb.ContextNoFlags)
	keymap, err := xkbCtx.NewKeymapFromNames(&xkb.RuleNames{
		Layout:  spec.Layout,
		Variant: spec.Variant,
	})
	if err != nil {
		return nil, fmt.Errorf("compile prompt layout %s: %w", formatLayout(spec.Layout, spec.Variant), err)
	}

	state := keymap.NewState()
	labels := make(map[string]string, len(logicalOrder))
	for _, logical := range logicalOrder {
		labels[logical] = promptLabel(logical, promptHintForLogical(state, logical))
	}
	return labels, nil
}

func parseSetupKeymapLayoutSpec(raw string) (config.ShortcutLayoutSpec, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return config.ShortcutLayoutSpec{}, errors.New("layout is required")
	}

	open := strings.LastIndex(trimmed, "(")
	close := strings.LastIndex(trimmed, ")")
	if open == -1 && close == -1 {
		return config.ShortcutLayoutSpec{Layout: trimmed}, nil
	}
	if open <= 0 || close != len(trimmed)-1 || close < open {
		return config.ShortcutLayoutSpec{}, fmt.Errorf("invalid layout %q", raw)
	}

	layout := strings.TrimSpace(trimmed[:open])
	variant := strings.TrimSpace(trimmed[open+1 : close])
	if layout == "" || variant == "" {
		return config.ShortcutLayoutSpec{}, fmt.Errorf("invalid layout %q", raw)
	}

	return config.ShortcutLayoutSpec{Layout: layout, Variant: variant}, nil
}

func promptHintForLogical(state *xkb.State, logical string) string {
	code, ok := promptKeycode(logical)
	if !ok {
		return ""
	}

	ch := state.KeyGetUTF32(xkb.Keycode(uint32(code) + setupKeymapXKBKeycodeBase))
	if !isPromptHintRune(ch) {
		return ""
	}
	return string(ch)
}

func promptKeycode(logical string) (uint16, bool) {
	switch strings.ToLower(strings.TrimSpace(logical)) {
	case "grv":
		return config.KeyGrv, true
	case "lsft":
		return config.KeyLeftShift, true
	case "rsft":
		return config.KeyRightShift, true
	case "lctl":
		return config.KeyLeftCtrl, true
	case "rctl":
		return config.KeyRightCtrl, true
	case "lmet":
		return config.KeyLeftMeta, true
	default:
		code, err := config.ParseKeyName(logical)
		if err != nil {
			return 0, false
		}
		return code, true
	}
}

func isPromptHintRune(ch rune) bool {
	return ch != 0 && ch != ' ' && unicode.IsPrint(ch)
}

func captureLayout(ctx context.Context, prompts promptReader, sequences sequenceReader, out io.Writer, logicalOrder []string, promptLabels map[string]string) ([]capturedKey, error) {
	keys := make([]capturedKey, 0, len(logicalOrder))

	_, _ = fmt.Fprintf(out, "Press Enter to arm each capture. After arming, press the key to record it.\n")
	_, _ = fmt.Fprintf(out, "Type 'skip' then Enter to skip a key. Ctrl+C or SIGTERM aborts.\n\n")

	for i, logical := range logicalOrder {
		for {
			_, _ = fmt.Fprintf(out, "[%2d/%2d] %-13s arm: ", i+1, len(logicalOrder), promptLabelForDisplay(logical, promptLabels))
			line, err := prompts.ReadLine(ctx)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return nil, errors.New("capture interrupted")
				}
				return nil, fmt.Errorf("read prompt input: %w", err)
			}

			command := strings.ToLower(strings.TrimSpace(line))
			switch command {
			case "", "go":
				_, _ = fmt.Fprintf(out, "        press key now")
				if !logicalIsEnter(logical) {
					_, _ = fmt.Fprintf(out, " (Enter skips)")
				}
				_, _ = fmt.Fprint(out, ": ")

				seq, err := sequences.Next(ctx)
				if err != nil {
					if errors.Is(err, context.Canceled) {
						return nil, errors.New("capture interrupted")
					}
					return nil, fmt.Errorf("read key sequence: %w", err)
				}
				if isInterruptSequence(seq) {
					return nil, errors.New("capture interrupted")
				}
				if isEnterSequence(seq) && !logicalIsEnter(logical) {
					key := capturedKey{Logical: logical, Skipped: true}
					keys = append(keys, key)
					_, _ = fmt.Fprintln(out, "skip")
				} else {
					key := buildCapturedKey(logical, seq)
					keys = append(keys, key)
					_, _ = fmt.Fprintln(out, key.Escaped)
				}

			case "skip", "s":
				key := capturedKey{Logical: logical, Skipped: true}
				keys = append(keys, key)
				_, _ = fmt.Fprintln(out, "        skip")

			default:
				_, _ = fmt.Fprintf(out, "        unknown command %q; press Enter to arm or type skip\n", command)
				continue
			}
			break
		}
	}

	return keys, nil
}

func buildCapturedKey(logical string, seq []byte) capturedKey {
	key := capturedKey{
		Logical: logical,
		Bytes:   append([]byte(nil), seq...),
		Escaped: strconv.QuoteToASCII(string(seq)),
	}

	if utf8.Valid(seq) {
		text := string(seq)
		if isPrintableText(text) {
			key.Text = text
		}
	}
	return key
}

func isPrintableText(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsPrint(r) && r != ' ' {
			return false
		}
	}
	return true
}

func logicalIsEnter(logical string) bool {
	switch strings.ToLower(strings.TrimSpace(logical)) {
	case "ret", "enter":
		return true
	default:
		return false
	}
}

func promptLabel(logical string, hint string) string {
	base := promptBaseLabel(logical)
	if hint == "" || strings.EqualFold(base, hint) {
		return base
	}
	return fmt.Sprintf("%s (%s)", base, hint)
}

func promptLabelForDisplay(logical string, promptLabels map[string]string) string {
	if label, ok := promptLabels[logical]; ok && strings.TrimSpace(label) != "" {
		return label
	}
	return promptLabel(logical, "")
}

func promptBaseLabel(logical string) string {
	normalized := strings.ToLower(strings.TrimSpace(logical))
	if label, ok := logicalPromptBaseLabels[normalized]; ok {
		return label
	}
	return logical
}

func isInterruptSequence(seq []byte) bool {
	return len(seq) == 1 && seq[0] == 0x03
}

func isEnterSequence(seq []byte) bool {
	return len(seq) == 1 && (seq[0] == '\r' || seq[0] == '\n')
}

func (r *terminalPromptReader) ReadLine(ctx context.Context) (string, error) {
	if err := waitForReadable(ctx, r.fd, 0); err != nil {
		return "", err
	}
	line, err := r.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return line, nil
}

func (r *terminalSequenceReader) Next(ctx context.Context) ([]byte, error) {
	state, err := makeRaw(r.fd)
	if err != nil {
		return nil, fmt.Errorf("switch stdin to raw mode: %w", err)
	}
	defer func() {
		_ = restoreTermios(r.fd, state)
	}()

	first, err := readByte(ctx, r.file, r.fd, 0)
	if err != nil {
		return nil, err
	}
	seq := []byte{first}

	if first == 0x1b {
		for {
			next, ok, err := readOptionalByte(ctx, r.file, r.fd, 35*time.Millisecond)
			if err != nil {
				return nil, err
			}
			if !ok {
				return seq, nil
			}
			seq = append(seq, next)
		}
	}

	if need := utf8SequenceLength(first); need > 1 {
		for len(seq) < need {
			next, err := readByte(ctx, r.file, r.fd, 0)
			if err != nil {
				return nil, err
			}
			seq = append(seq, next)
		}
	}

	return seq, nil
}

func writeLayoutJSON(path string, layout layoutFile, stdout io.Writer) error {
	writer := stdout
	var closer io.Closer

	trimmedPath := strings.TrimSpace(path)
	if trimmedPath != "" && trimmedPath != "-" {
		f, err := os.Create(trimmedPath)
		if err != nil {
			return fmt.Errorf("create %s: %w", trimmedPath, err)
		}
		writer = f
		closer = f
	}
	if closer != nil {
		defer func() {
			_ = closer.Close()
		}()
	}

	enc := json.NewEncoder(writer)
	enc.SetIndent("", "  ")
	if err := enc.Encode(layout); err != nil {
		return fmt.Errorf("write json: %w", err)
	}
	return nil
}

func makeRaw(fd int) (termiosState, error) {
	state, err := readTermios(fd)
	if err != nil {
		return termiosState{}, err
	}

	raw := state
	raw.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	raw.Oflag &^= syscall.OPOST
	raw.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Cflag &^= syscall.CSIZE | syscall.PARENB
	raw.Cflag |= syscall.CS8
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0

	if err := writeTermios(fd, raw); err != nil {
		return termiosState{}, err
	}
	return termiosState{value: state}, nil
}

func restoreTermios(fd int, state termiosState) error {
	return writeTermios(fd, state.value)
}

func readTermios(fd int) (syscall.Termios, error) {
	var value syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&value)))
	if errno != 0 {
		return syscall.Termios{}, errno
	}
	return value, nil
}

func writeTermios(fd int, value syscall.Termios) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&value)))
	if errno != 0 {
		return errno
	}
	return nil
}

func readByte(ctx context.Context, file *os.File, fd int, timeout time.Duration) (byte, error) {
	if err := waitForReadable(ctx, fd, timeout); err != nil {
		return 0, err
	}

	var buf [1]byte
	n, err := file.Read(buf[:])
	if err != nil {
		return 0, err
	}
	if n != 1 {
		return 0, io.ErrUnexpectedEOF
	}
	return buf[0], nil
}

func readOptionalByte(ctx context.Context, file *os.File, fd int, timeout time.Duration) (byte, bool, error) {
	if err := waitForReadable(ctx, fd, timeout); err != nil {
		if errors.Is(err, errReadTimeout) {
			return 0, false, nil
		}
		return 0, false, err
	}

	b, err := readByte(ctx, file, fd, 0)
	if err != nil {
		return 0, false, err
	}
	return b, true, nil
}

var errReadTimeout = errors.New("read timeout")

func waitForReadable(ctx context.Context, fd int, timeout time.Duration) error {
	step := 100 * time.Millisecond
	if timeout > 0 && timeout < step {
		step = timeout
	}

	deadline := time.Time{}
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		wait := step
		if !deadline.IsZero() {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				return errReadTimeout
			}
			if remaining < wait {
				wait = remaining
			}
		}

		var fds syscall.FdSet
		fdSet(fd, &fds)
		tv := syscall.NsecToTimeval(wait.Nanoseconds())
		n, err := syscall.Select(fd+1, &fds, nil, nil, &tv)
		if err != nil {
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			return err
		}
		if n > 0 {
			return nil
		}
	}
}

func fdSet(fd int, set *syscall.FdSet) {
	set.Bits[fd/64] |= 1 << (uint(fd) % 64)
}

func utf8SequenceLength(first byte) int {
	switch {
	case first&0x80 == 0x00:
		return 1
	case first&0xe0 == 0xc0:
		return 2
	case first&0xf0 == 0xe0:
		return 3
	case first&0xf8 == 0xf0:
		return 4
	default:
		return 1
	}
}
