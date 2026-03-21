package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"time"
)

var (
	runRemapFn         = run
	runSetupKeymapFn   = runSetupKeymap
	generateXComposeFn = generateXComposeFile
)

func runCLI(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "setup-keymap":
			err := runSetupKeymapFn(args[1:])
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		case "help":
			printTopLevelUsage(os.Stdout)
			return nil
		}
	}

	err := runRemapCommand(args)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return err
}

func runRemapCommand(args []string) error {
	fs := flag.NewFlagSet("altremap", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	devicePath := fs.String("device", defaultDevicePath, "input keyboard device path")
	configPath := fs.String("config", "altremap.yaml", "optional YAML config file path")
	generateXCompose := fs.String("generate-xcompose", "", "write deterministic XCompose entries to this path and exit")
	composeDelay := fs.Duration("compose-delay", 5*time.Millisecond, "delay between compose key taps")
	grab := fs.Bool("grab", true, "grab input device so physical events are not duplicated")
	verbose := fs.Bool("verbose", false, "enable verbose logs")

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: %s [flags]\n", fs.Name())
		fs.PrintDefaults()
		fmt.Fprintln(fs.Output(), "")
		fmt.Fprintf(fs.Output(), "Subcommand:\n  %s setup-keymap [flags]\n", fs.Name())
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
	if *generateXCompose != "" {
		return generateXComposeFn(*configPath, *generateXCompose)
	}

	return runRemapFn(*devicePath, *configPath, *composeDelay, *grab, *verbose)
}

func printTopLevelUsage(w *os.File) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  altremap [flags]")
	fmt.Fprintln(w, "  altremap setup-keymap [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Run `altremap -h` for remapper flags.")
	fmt.Fprintln(w, "Run `altremap setup-keymap -h` for keymap capture flags.")
}
