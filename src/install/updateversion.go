package main

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var (
	semverPattern       = regexp.MustCompile(`^(?:v)?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	versionValuePattern = regexp.MustCompile(`(?m)^const Value = "[^"]*"$`)
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	version, err := normalizeVersion(os.Getenv("VERSION"))
	if err != nil {
		return err
	}
	if err := stampVersion("src/version/version.go", version); err != nil {
		return err
	}
	fmt.Printf("updated version.Value to %s\n", version)
	return nil
}

func normalizeVersion(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	match := semverPattern.FindStringSubmatch(raw)
	if match == nil {
		return "", fmt.Errorf("VERSION must be a semantic version such as v11.2.3: %q", raw)
	}
	return strings.TrimPrefix(raw, "v"), nil
}

func stampVersion(filename, version string) error {
	if version == "" {
		return errors.New("version must not be empty")
	}

	contents, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("read %s: %w", filename, err)
	}
	if matches := versionValuePattern.FindAll(contents, -1); len(matches) != 1 {
		return fmt.Errorf("expected exactly one version.Value declaration in %s, found %d", filename, len(matches))
	}

	replacement := []byte(fmt.Sprintf(`const Value = %q`, version))
	updated := versionValuePattern.ReplaceAll(contents, replacement)
	if string(updated) == string(contents) {
		return nil
	}

	info, err := os.Stat(filename)
	if err != nil {
		return fmt.Errorf("stat %s: %w", filename, err)
	}
	if err := os.WriteFile(filename, updated, info.Mode().Perm()); err != nil {
		return fmt.Errorf("write %s: %w", filename, err)
	}
	return nil
}
