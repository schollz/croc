package update

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"
)

// Options describes one of the two complementary update commands. Alternate
// endpoints and runners are injection points for tests, never CLI/env options.
type Options struct {
	Tool, Version                     string
	Check, Yes                        bool
	InstallVersion                    string // hidden elevated operation, pinned by the parent
	Writer                            io.Writer
	ProgressWriter                    io.Writer
	Confirm                           func(context.Context, string) (bool, error)
	Executable, Invoked               string
	Client                            *http.Client
	LatestEndpoint, DownloadURLFormat string
	Elevate                           func(context.Context, string, []string) error
}

func (o Options) Run(ctx context.Context) error {
	if o.Tool != Croc && o.Tool != Helper {
		return errors.New("invalid updater tool")
	}
	if o.Writer == nil {
		o.Writer = os.Stdout
	}
	if o.ProgressWriter == nil {
		o.ProgressWriter = os.Stderr
	}
	if o.Client == nil {
		o.Client = NewHTTPClient(2 * time.Minute)
	}
	if o.LatestEndpoint == "" {
		o.LatestEndpoint = LatestReleaseEndpoint
	}
	if o.DownloadURLFormat == "" {
		o.DownloadURLFormat = releaseDownloadURLFormat
	}
	if o.Confirm == nil {
		o.Confirm = Confirm
	}
	if o.Elevate == nil {
		o.Elevate = elevate
	}
	executable := o.Executable
	if executable == "" {
		var err error
		executable, err = Executable()
		if err != nil {
			return fmt.Errorf("locate running executable: %w", err)
		}
	}
	executable, err := filepath.Abs(executable)
	if err != nil {
		return err
	}
	if o.Invoked == "" {
		o.Invoked = os.Args[0]
	}
	targetTool := Croc
	if o.Tool == Croc {
		targetTool = Helper
	}
	target := filepath.Join(filepath.Dir(executable), BinaryName(targetTool))
	if !o.Check {
		if guidance := ManagedGuidance(executable); guidance != "" {
			if o.InstallVersion != "" {
				return errors.New(guidance)
			}
			fmt.Fprintln(o.Writer, guidance)
			return nil
		}
		if invocationUsesSymlink(o.Invoked, executable) {
			return errors.New("the running command resolves through a symlink; run the executable by its real path to update")
		}
		if err := ValidateStandalone(executable); err != nil {
			return err
		}
		if err := ValidateStandalone(target); err != nil {
			return err
		}
	}
	if !ValidVersion(o.Version) {
		return fmt.Errorf("cannot update development build %q; install a stable release first", o.Version)
	}
	latest := o.InstallVersion
	if latest != "" {
		if !ValidVersion(latest) || o.Check {
			return errors.New("invalid internal installation request")
		}
	} else {
		checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		latest, err = FetchLatest(checkCtx, o.Client, o.LatestEndpoint)
		if err != nil {
			return fmt.Errorf("check for a newer croc release: %w", err)
		}
	}
	current, err := installedVersion(ctx, target, targetTool)
	if err != nil {
		return err
	}
	if o.Tool == Croc {
		reportVersion(o.Writer, Croc, o.Version, latest)
	}
	reportVersion(o.Writer, targetTool, current, latest)
	if o.Check {
		return nil
	}
	if current == "" || Newer(latest, current) {
		if !o.Yes && o.InstallVersion == "" {
			confirmed, err := o.Confirm(ctx, fmt.Sprintf("Install %s v%s at %s? (y/N) ", targetTool, latest, target))
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Fprintln(o.Writer, "Update cancelled.")
				return nil
			}
		}
		changed := false
		changed, err = install(ctx, target, executable, targetTool, latest, o.Client, o.DownloadURLFormat, o.ProgressWriter)
		if err != nil {
			if !errors.Is(err, os.ErrPermission) || o.InstallVersion != "" {
				return err
			}
			args := []string{"--internal-install-version", latest}
			if o.Tool == Croc {
				args = append([]string{"update"}, args...)
			}
			fmt.Fprintf(o.Writer, "Installing %s requires elevated permissions for %s.\n", targetTool, target)
			if err := o.Elevate(ctx, executable, args); err != nil {
				return fmt.Errorf("elevated installation failed: %w", err)
			}
			installed, err := installedVersion(ctx, target, targetTool)
			if err != nil {
				return err
			}
			if installed == "" || Newer(latest, installed) {
				return errors.New("elevated installer did not install the requested release")
			}
			// Windows UAC can launch the installer in another console. Report
			// success here too, in the terminal that started the update.
			fmt.Fprintf(o.Writer, "Installed %s v%s at %s.\n", targetTool, installed, target)
		} else if changed {
			fmt.Fprintf(o.Writer, "Installed %s v%s at %s.\n", targetTool, latest, target)
		} else {
			current, err := installedVersion(ctx, target, targetTool)
			if err != nil {
				return err
			}
			reportVersion(o.Writer, targetTool, current, latest)
		}
	}
	if o.Tool == Croc && o.InstallVersion == "" {
		var args []string
		if o.Yes {
			args = append(args, "--yes")
		}
		fmt.Fprintf(o.Writer, "Run: %s\n", ShellCommand(target, args...))
	}
	return nil
}

func reportVersion(writer io.Writer, tool, current, latest string) {
	switch {
	case current == "":
		fmt.Fprintf(writer, "%s is not installed (latest: v%s).\n", tool, latest)
	case Newer(latest, current):
		fmt.Fprintf(writer, "%s v%s is available (current: v%s).\n", tool, latest, current)
	default:
		fmt.Fprintf(writer, "%s v%s is up to date.\n", tool, current)
	}
}

func Confirm(ctx context.Context, prompt string) (bool, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false, errors.New("update confirmation requires an interactive terminal; rerun with --yes")
	}
	fmt.Fprint(os.Stderr, prompt)
	type result struct {
		line string
		err  error
	}
	done := make(chan result, 1)
	go func() { line, err := bufio.NewReader(os.Stdin).ReadString('\n'); done <- result{line, err} }()
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case r := <-done:
		if r.err != nil {
			return false, r.err
		}
		line := strings.TrimSpace(r.line)
		return strings.EqualFold(line, "y") || strings.EqualFold(line, "yes"), nil
	}
}

// A bounded writer also protects version checks against a damaged executable.
type versionOutput struct{ bytes.Buffer }

func (w *versionOutput) Write(p []byte) (int, error) {
	if len(p)+w.Len() > 4096 {
		return 0, errors.New("version output is too large")
	}
	return w.Buffer.Write(p)
}

func installedVersion(ctx context.Context, target, tool string) (string, error) {
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("refusing non-regular or symlinked executable %q", target)
	}
	return executableVersion(ctx, target, tool)
}

func executableVersion(ctx context.Context, target, tool string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, target, "--version")
	output := &versionOutput{}
	command.Stdout, command.Stderr = output, output
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("inspect %s version: %w", target, err)
	}
	text := strings.TrimSpace(output.String())
	prefix := tool + " version "
	v := strings.TrimPrefix(text, prefix)
	if v == text || !ValidVersion(v) {
		return "", fmt.Errorf("%s reports an invalid version: %q", target, text)
	}
	return v, nil
}

func install(ctx context.Context, target, sibling, tool, version string, client *http.Client, urlFormat string, progress io.Writer) (bool, error) {
	if err := ValidateStandalone(target); err != nil {
		return false, err
	}
	if err := ValidateStandalone(sibling); err != nil {
		return false, err
	}
	asset, err := AssetName(version)
	if err != nil {
		return false, err
	}
	unlock, err := lockDirectory(filepath.Dir(target))
	if err != nil {
		return false, fmt.Errorf("lock installation directory: %w", err)
	}
	defer unlock()
	currentVersion, err := installedVersion(ctx, target, tool)
	if err != nil {
		return false, err
	}
	if currentVersion != "" && !Newer(version, currentVersion) {
		return false, nil
	}
	current, err := os.Lstat(target)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	metadata := current
	if metadata == nil {
		metadata, err = os.Lstat(sibling)
		if err != nil {
			return false, err
		}
	}
	staged, err := os.CreateTemp(filepath.Dir(target), ".croc-update-*.exe")
	if err != nil {
		return false, fmt.Errorf("stage %s: %w", tool, err)
	}
	name := staged.Name()
	defer os.Remove(name)
	defer staged.Close()
	if err := setMetadata(staged, metadata); err != nil {
		return false, fmt.Errorf("preserve executable permissions: %w", err)
	}
	archive, err := downloadUpdate(ctx, client, fmt.Sprintf(urlFormat, version, asset), maxUpdateArchiveSize, progress)
	if err != nil {
		return false, fmt.Errorf("download %s update: %w", tool, err)
	}
	checksums, err := downloadUpdate(ctx, client, fmt.Sprintf(urlFormat, version, "croc_v"+version+"_checksums.txt"), maxUpdateChecksumsSize)
	if err != nil {
		return false, fmt.Errorf("download release checksums: %w", err)
	}
	if err := verifyUpdateChecksum(asset, archive, checksums); err != nil {
		return false, err
	}
	if err := extractBinary(asset, archive, BinaryName(tool), staged); err != nil {
		return false, err
	}
	if err := staged.Sync(); err != nil {
		return false, err
	}
	if err := staged.Close(); err != nil {
		return false, err
	}
	got, err := executableVersion(ctx, name, tool)
	if err != nil {
		return false, fmt.Errorf("verify staged %s: %w", tool, err)
	}
	if got != version {
		return false, fmt.Errorf("staged %s reports %s; expected %s", tool, got, version)
	}
	// Check identity again after the download and execution, before publication.
	if err := ValidateStandalone(target); err != nil {
		return false, err
	}
	now, err := os.Lstat(target)
	if current == nil {
		if !os.IsNotExist(err) {
			return false, errors.New("destination appeared during update; retry")
		}
	} else if err != nil || !os.SameFile(current, now) {
		return false, errors.New("destination changed during update; retry")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := replaceFile(name, target, current != nil); err != nil {
		return false, fmt.Errorf("replace %s: %w", target, err)
	}
	return true, nil
}
