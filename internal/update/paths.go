package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const Croc = "croc"
const Helper = "croc-update"

func BinaryName(tool string) string {
	if runtime.GOOS == "windows" {
		return tool + ".exe"
	}
	return tool
}

// CrocTarget prefers the command active in this shell. An explicit path pins
// the selection across elevation and when croc prints its follow-up command.
func CrocTarget(executable, explicit string) (string, error) {
	target := explicit
	if target == "" {
		var err error
		target, err = exec.LookPath(Croc)
		if err != nil && !errors.Is(err, exec.ErrNotFound) {
			return "", fmt.Errorf("locate croc on PATH (use --path to select explicitly): %w", err)
		}
		if errors.Is(err, exec.ErrNotFound) {
			target = filepath.Join(filepath.Dir(executable), BinaryName(Croc))
		}
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	// Normalize parent-directory aliases such as macOS /var without following
	// an executable symlink. Such a destination must remain protected.
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(abs)), nil
}

func Executable() (string, error) {
	name, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(name)
}

func invocationUsesSymlink(invoked, target string) bool {
	if !strings.ContainsAny(invoked, `/\`) {
		var err error
		invoked, err = exec.LookPath(invoked)
		if err != nil {
			return false
		}
	}
	abs, err := filepath.Abs(invoked)
	if err != nil {
		return false
	}
	// macOS routinely spells /private/var as /var. A symlink in a parent
	// directory is harmless; protect executable aliases themselves.
	info, err := os.Lstat(abs)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	resolved, err := filepath.EvalSymlinks(abs)
	return err == nil && filepath.Clean(resolved) == filepath.Clean(target)
}

// ManagedGuidance is shared by both binaries, including the elevated child.
// Convert the helper's name to croc before applying package ownership rules.
func ManagedGuidance(target string) string {
	target = filepath.Join(filepath.Dir(target), BinaryName(Croc))
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		target = resolved
	}
	n := filepath.ToSlash(strings.ToLower(target))
	n = strings.TrimSuffix(n, ".exe")
	switch {
	case strings.Contains(n, "/cellar/croc/") || strings.Contains(n, "/linuxbrew/"):
		return "Upgrade with Homebrew: brew upgrade croc"
	case strings.HasPrefix(n, "/nix/store/"):
		return "Upgrade croc through the Nix profile or NixOS configuration that owns it."
	case strings.Contains(n, "/scoop/apps/croc/"):
		return "Upgrade with Scoop: scoop update croc"
	case strings.Contains(n, "/chocolatey/"):
		return "Upgrade with Chocolatey: choco upgrade croc"
	case strings.Contains(n, "/conda/") || strings.Contains(n, "/miniconda") || strings.Contains(n, "/anaconda"):
		return "Upgrade with Conda: conda update --channel conda-forge croc"
	case n == "/opt/local/bin/croc":
		return "Upgrade with MacPorts: sudo port selfupdate && sudo port upgrade croc"
	case strings.Contains(n, "/com.termux/files/usr/bin/croc"):
		return "Upgrade with Termux pkg: pkg upgrade croc"
	case n == "/usr/bin/croc" || n == "/bin/croc":
		return "Upgrade croc with the system package manager that installed it (for example apt, dnf, zypper, pacman, or apk)."
	}
	tool, args, guidance := ownershipQuery(runtime.GOOS, target)
	if tool != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if executable, err := exec.LookPath(tool); err == nil {
			output, err := exec.CommandContext(ctx, executable, args...).CombinedOutput()
			if err == nil && len(strings.TrimSpace(string(output))) > 0 {
				return guidance
			}
			// These package tools use status 1 for a path with no owner.
			var exit *exec.ExitError
			if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
				return "Could not determine package ownership; inspect it with " + tool + " before updating."
			}
		}
	}
	return ""
}

// BSD package prefixes overlap the standalone installer's /usr/local default.
func ownershipQuery(goos, target string) (string, []string, string) {
	switch goos {
	case "freebsd", "dragonfly":
		return "pkg", []string{"which", "-q", target}, "Upgrade with pkg: pkg upgrade croc"
	case "openbsd":
		return "pkg_info", []string{"-E", target}, "Upgrade with OpenBSD pkg_add: doas pkg_add -u croc"
	case "netbsd":
		return "pkg_info", []string{"-F", target}, "Upgrade croc through the pkgsrc or pkgin installation that owns it."
	default:
		return "", nil, ""
	}
}

func ValidateStandalone(target string) error {
	if guidance := ManagedGuidance(target); guidance != "" {
		return fmt.Errorf("refusing to modify a package-managed installation. %s", guidance)
	}
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to replace non-regular or symlinked executable %q", target)
	}
	return nil
}

func ShellCommand(executable string, args ...string) string {
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	if runtime.GOOS == "windows" {
		quote = func(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
	}
	parts := []string{quote(executable)}
	for _, arg := range args {
		parts = append(parts, quote(arg))
	}
	prefix := ""
	if runtime.GOOS == "windows" {
		prefix = "& "
	}
	return prefix + strings.Join(parts, " ")
}

// Recovery instructions use public flags. Only the automatic child needs the
// hidden, pinned installation protocol.
func publicUpdateArgs(args []string) []string {
	result := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--internal-install-version" {
			i++
			continue
		}
		result = append(result, args[i])
	}
	return append(result, "--yes")
}
