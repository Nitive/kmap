package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

var (
	runStartFn         = run
	runSetupKeymapFn   = runSetupKeymap
	generateXComposeFn = generateXComposeFile
)

func runCLI(args []string) error {
	if len(args) == 0 {
		printTopLevelUsage(os.Stderr)
		return errors.New("missing command")
	}

	switch args[0] {
	case "start":
		err := runStartCommand(args[1:])
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	case "setup-keymap":
		err := runSetupKeymapFn(args[1:])
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	case "generate-xcompose":
		err := runGenerateXComposeCommand(args[1:])
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	case "help", "-h", "--help":
		printTopLevelUsage(os.Stdout)
		return nil
	default:
		printTopLevelUsage(os.Stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runStartCommand(args []string) error {
	fs := flag.NewFlagSet("kmap start", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	devicePath := fs.String("device", defaultDevicePath, "input keyboard device path")
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

	return generateXComposeFn(*configPath, resolvedOutput)
}

func printTopLevelUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  kmap start [flags]")
	fmt.Fprintln(w, "  kmap setup-keymap [flags]")
	fmt.Fprintln(w, "  kmap generate-xcompose --output <path> [--config <path>]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Run `kmap <command> -h` for command-specific flags.")
}
