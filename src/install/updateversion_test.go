package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeVersion(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "tag", input: "v11.2.3", want: "11.2.3"},
		{name: "plain version", input: "11.2.3", want: "11.2.3"},
		{name: "surrounding whitespace", input: "  v11.2.3\n", want: "11.2.3"},
		{name: "missing", wantErr: true},
		{name: "missing patch", input: "v11.2", wantErr: true},
		{name: "leading zero", input: "v11.02.3", wantErr: true},
		{name: "prerelease", input: "v11.2.3-rc.1", wantErr: true},
		{name: "unexpected text", input: "release-v11.2.3", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeVersion(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("normalizeVersion(%q) succeeded, want error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeVersion(%q): %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("normalizeVersion(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestStampVersion(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "version.go")
	original := "package version\n\nconst Value = \"11.2.2\"\n"
	if err := os.WriteFile(filename, []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}

	if err := stampVersion(filename, "11.2.3"); err != nil {
		t.Fatalf("stamp version: %v", err)
	}
	want := "package version\n\nconst Value = \"11.2.3\"\n"
	assertFileContents(t, filename, want)

	// Reapplying the same version is an intentional no-op for workflow reruns.
	if err := stampVersion(filename, "11.2.3"); err != nil {
		t.Fatalf("stamp same version: %v", err)
	}
	assertFileContents(t, filename, want)

	info, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("file permissions = %o, want 640", got)
	}
}

func TestStampVersionRejectsUnexpectedFiles(t *testing.T) {
	tests := []struct {
		name     string
		contents string
	}{
		{name: "missing declaration", contents: "package version\n"},
		{name: "multiple declarations", contents: "const Value = \"1.0.0\"\nconst Value = \"2.0.0\"\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "version.go")
			if err := os.WriteFile(filename, []byte(tt.contents), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := stampVersion(filename, "11.2.3"); err == nil {
				t.Fatal("stampVersion succeeded, want error")
			}
		})
	}
}

func assertFileContents(t *testing.T, filename, want string) {
	t.Helper()
	contents, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(contents); got != want {
		t.Fatalf("file contents = %q, want %q", got, want)
	}
}
