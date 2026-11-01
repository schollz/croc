package cli

import (
	"errors"
	"io"
	"os"

	internalcli "github.com/schollz/croc/v11/internal/cli"
	"github.com/schollz/croc/v11/internal/update"
)

func updateCommand(c *internalcli.Context) error {
	if c.Bool("register-installer") {
		target, err := update.Executable()
		if err != nil {
			return err
		}
		if guidance := update.ManagedGuidance(target); guidance != "" {
			return errors.New("cannot register a package-managed executable. " + guidance)
		}
		return update.ValidateStandalone(target)
	}
	writer := io.Writer(os.Stdout)
	if c.App != nil && c.App.Writer != nil {
		writer = c.App.Writer
	}
	return (update.Options{Tool: update.Croc, Version: Version, Check: c.Bool("check"), Yes: c.Bool("yes"), InstallVersion: c.String("internal-install-version"), Writer: writer}).Run(c.Context)
}
