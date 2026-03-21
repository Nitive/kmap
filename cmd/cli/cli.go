package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"keyboard/pkg/config"
	"keyboard/pkg/daemon"
	"keyboard/pkg/daemon/shortcut"
	"keyboard/pkg/xcompose"
)

var (
	runStartFn            = runStart
	generateFn            = xcompose.GenerateFile
	defaultXComposePathFn = xcompose.DefaultOutputPath
	daemonStartFn         = daemon.Start
	loadRuntimeFn         = config.LoadRuntime
	switchValidateFn      = func(ctx context.Context, cfg config.Runtime) (shortcut.ValidationInfo, error) {
		return shortcut.ValidateSwitchConfig(ctx, cfg)
	}
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
	"generate-xcompose": {
		Name:        "generate-xcompose",
		Description: "Generate XCompose rules for mapped symbols",
		Run:         runGenerateXComposeCommand,
	},
	"validate-config": {
		Name:        "validate-config",
		Description: "Validate config parsing and KDE layout switching setup",
		Run:         runValidateConfigCommand,
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
	xcomposePath, err := defaultXComposePathFn()
	if err != nil {
		return fmt.Errorf("resolve XCompose path: %w", err)
	}
	if err := generateFn(configPath, xcomposePath); err != nil {
		return fmt.Errorf("generate XCompose %s: %w", xcomposePath, err)
	}

	return daemonStartFn(daemon.StartOptions{
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
	configPath := fs.String("config", config.DefaultConfigPath, "YAML config file path")
	composeDelay := fs.Duration("compose-delay", 5*time.Millisecond, "delay between compose key taps")
	grab := fs.Bool("grab", true, "grab input device so physical events are not duplicated")
	verbose := fs.Bool("verbose", false, "enable verbose logs")

	fs.Usage = func() {
		_, _ = fmt.Fprintf(fs.Output(), "Usage: kmap start [flags]\n")
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

	configPath := fs.String("config", config.DefaultConfigPath, "YAML config file path")
	outputPath := fs.String("output", "", "write deterministic XCompose entries to this path")

	fs.Usage = func() {
		_, _ = fmt.Fprintf(fs.Output(), "Usage: kmap generate-xcompose --output <path> [--config <path>]\n")
		_, _ = fmt.Fprintf(fs.Output(), "   or: kmap generate-xcompose <path> [--config <path>]\n")
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

func runValidateConfig(configPath string, out io.Writer) error {
	if _, err := os.Stat(configPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("config file %s does not exist", configPath)
		}
		return fmt.Errorf("stat config %s: %w", configPath, err)
	}

	cfg, err := loadRuntimeFn(configPath)
	if err != nil {
		return err
	}

	if cfg.ShortcutLayout == nil && len(cfg.TapLayoutSwitches) == 0 {
		_, _ = fmt.Fprintf(out, "config OK: %s\n", configPath)
		return nil
	}

	info, err := switchValidateFn(context.Background(), cfg)
	if err != nil {
		return fmt.Errorf("layout switch validation failed: %w", err)
	}

	details := ""
	if cfg.ShortcutLayout != nil {
		details = fmt.Sprintf(
			"shortcut current=%s target=%s target_index=%d",
			formatLayout(info.Current.Layout, info.Current.Variant),
			formatLayout(info.ShortcutTarget.Layout, info.ShortcutTarget.Variant),
			info.ShortcutTargetIndex,
		)
	} else {
		details = fmt.Sprintf("current=%s", formatLayout(info.Current.Layout, info.Current.Variant))
	}
	if len(info.TapSwitches) > 0 {
		details += fmt.Sprintf(" tap_switches=%d", len(info.TapSwitches))
	}

	_, _ = fmt.Fprintf(out, "config OK: %s (%s)\n", configPath, details)
	return nil
}

func runValidateConfigCommand(args []string) error {
	fs := flag.NewFlagSet("kmap validate-config", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	configPath := fs.String("config", config.DefaultConfigPath, "YAML config file path")

	fs.Usage = func() {
		_, _ = fmt.Fprintf(fs.Output(), "Usage: kmap validate-config [--config <path>]\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}

	return runValidateConfig(*configPath, os.Stdout)
}

func formatLayout(layout string, variant string) string {
	if variant == "" {
		return layout
	}
	return fmt.Sprintf("%s(%s)", layout, variant)
}

func printTopLevelUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Usage: kmap <command> [flags]")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Commands:")

	keys := make([]string, 0, len(commands))
	for k := range commands {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		cmd := commands[k]
		_, _ = fmt.Fprintf(w, "  %-18s %s\n", cmd.Name, cmd.Description)
	}
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Run `kmap <command> -h` for command-specific flags.")
}
