package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/schollz/croc/v11/internal/update"
	"github.com/schollz/croc/v11/src/version"
)

func run(ctx context.Context, args []string, writer io.Writer) error {
	flags := flag.NewFlagSet("croc-update", flag.ContinueOnError)
	flags.SetOutput(writer)
	check := flags.Bool("check", false, "check for a newer croc release without installing")
	yes := flags.Bool("yes", false, "install without confirmation (OS authentication may still be required)")
	showVersion := flags.Bool("version", false, "print the updater version")
	path := flags.String("path", "", "select a croc executable explicitly instead of using PATH")
	installVersion := flags.String("internal-install-version", "", "")
	flags.Usage = func() {
		fmt.Fprintln(writer, "Usage: croc-update [--check] [--yes] [--path EXECUTABLE]\nUpdate the croc active on PATH; otherwise install beside this executable.\n\n  --check    check without installing\n  --yes      skip confirmation; OS authentication may still be required\n  --path     select a croc executable explicitly\n  --version  print the updater version")
	}
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	if *showVersion {
		fmt.Fprintln(writer, "croc-update version "+version.Value)
		return nil
	}
	return (update.Options{Tool: update.Helper, Version: version.Value, Check: *check, Yes: *yes, CrocPath: *path, InstallVersion: *installVersion, Writer: writer}).Run(ctx)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
