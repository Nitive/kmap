package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"keyboard/pkg/daemon"
	"keyboard/pkg/xcompose"
)

var (
	runStartFn       = runStart
	runSetupKeymapFn = runSetupKeymap
	generateFn       = xcompose.GenerateFile
)

type cliCommand struct {
	Name        string
	Description string
	Run         func(args []string) error
}

var commands = map[string]cliCommand{
	"start": {
		Name:        "start",
		Description: "Run the remapping daemon",
		Run:         runStartCommand,
	},
	"setup-keymap": {
		Name:        "setup-keymap",
		Description: "Interactively capture keyboard layout",
		Run:         func(args []string) error { return runSetupKeymapFn(args) },
	},
	"generate-xcompose": {
		Name:        "generate-xcompose",
		Description: "Generate XCompose rules for mapped symbols",
		Run:         runGenerateXComposeCommand,
	},
}

func Run(args []string) error {
	if len(args) == 0 {
		printTopLevelUsage(os.Stderr)
		return errors.New("missing command")
	}

	cmdName := args[0]
	if cmdName == "help" || cmdName == "-h" || cmdName == "--help" {
		printTopLevelUsage(os.Stdout)
		return nil
	}

	cmd, ok := commands[cmdName]
	if !ok {
		printTopLevelUsage(os.Stderr)
		return fmt.Errorf("unknown command %q", cmdName)
	}

	err := cmd.Run(args[1:])
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return err
}

func runStart(devicePath string, configPath string, composeDelay time.Duration, grab bool, verbose bool) error {
	return daemon.Start(daemon.StartOptions{
		DeviceOverride: devicePath,
		ConfigPath:     configPath,
		ComposeDelay:   composeDelay,
		Grab:           grab,
		Verbose:        verbose,
	})
}

func runStartCommand(args []string) error {
	fs := flag.NewFlagSet("kmap start", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	devicePath := fs.String("device", "", "input keyboard device path override (optional)")
	configPath := fs.String("config", "kmap.yaml", "YAML config file path")
	composeDelay := fs.Duration("compose-delay", 5*time.Millisecond, "delay between compose key taps")
	grab := fs.Bool("grab", true, "grab input device so physical events are not duplicated")
	verbose := fs.Bool("verbose", false, "enable verbose logs")

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: kmap start [flags]\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}
	if *composeDelay < 0 {
		return errors.New("compose-delay must be >= 0")
	}

	return runStartFn(*devicePath, *configPath, *composeDelay, *grab, *verbose)
}

func runGenerateXComposeCommand(args []string) error {
	fs := flag.NewFlagSet("kmap generate-xcompose", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	configPath := fs.String("config", "kmap.yaml", "YAML config file path")
	outputPath := fs.String("output", "", "write deterministic XCompose entries to this path")

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: kmap generate-xcompose --output <path> [--config <path>]\n")
		fmt.Fprintf(fs.Output(), "   or: kmap generate-xcompose <path> [--config <path>]\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	resolvedOutput := *outputPath
	switch {
	case resolvedOutput == "" && fs.NArg() == 1:
		resolvedOutput = fs.Arg(0)
	case resolvedOutput == "" && fs.NArg() == 0:
		return errors.New("output path is required")
	case resolvedOutput == "" && fs.NArg() > 1:
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	case resolvedOutput != "" && fs.NArg() > 0:
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}

	return generateFn(*configPath, resolvedOutput)
}

func printTopLevelUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: kmap <command> [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Commands:")

	keys := make([]string, 0, len(commands))
	for k := range commands {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		cmd := commands[k]
		fmt.Fprintf(w, "  %-18s %s\n", cmd.Name, cmd.Description)
	}
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Run `kmap <command> -h` for command-specific flags.")
}
