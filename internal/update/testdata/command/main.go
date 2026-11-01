// Test-only command: exercise real executable replacement without production
// flags or environment variables that could redirect trusted release downloads.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/schollz/croc/v11/internal/update"
)

var tool, version string

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--hold" {
		fmt.Println("holding")
		io.Copy(io.Discard, os.Stdin)
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("%s version %s\n", tool, version)
		return
	}
	args := os.Args[1:]
	if tool == update.Croc && len(args) > 0 && args[0] == "update" {
		args = args[1:]
	}
	flags := flag.NewFlagSet(tool, flag.ExitOnError)
	check := flags.Bool("check", false, "")
	yes := flags.Bool("yes", false, "")
	internal := flags.String("internal-install-version", "", "")
	path := flags.String("path", "", "")
	flags.Parse(args)
	endpoint := os.Getenv("CROC_UPDATE_TEST_URL")
	if endpoint == "" {
		executable, _ := os.Executable()
		data, _ := os.ReadFile(filepath.Join(filepath.Dir(executable), ".test-release-url"))
		endpoint = string(data)
	}
	err := (update.Options{Tool: tool, Version: version, Check: *check, Yes: *yes, CrocPath: *path, InstallVersion: *internal, LatestEndpoint: endpoint + "/latest", DownloadURLFormat: endpoint + "/v%s/%s"}).Run(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
