package receivefs

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestNormalizeRejectsUnsafePortablePaths(t *testing.T) {
	tests := []string{
		"../escape", "safe/../escape", "/absolute", `C:\absolute`,
		`\\server\share`, "control\x1bname", "line\nname", "NUL",
		"con.txt", "COM1.log", "LPT9", "file:stream", "trailing.",
		"trailing ", "zero\u200bwidth",
	}
	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			if _, err := Normalize(value, false); !errors.Is(err, ErrUnsafePath) {
				t.Fatalf("Normalize(%q) error = %v, want ErrUnsafePath", value, err)
			}
		})
	}
}

func TestNormalizeProducesPortableNFCPaths(t *testing.T) {
	got, err := Normalize("folder\\e\u0301.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	if got != "folder/é.txt" {
		t.Fatalf("normalized path = %q", got)
	}
	if got, err = Normalize("./folder//file.txt", false); err != nil || got != "folder/file.txt" {
		t.Fatalf("normalized repeated path = %q, %v", got, err)
	}
}

func TestValidateEntriesRejectsPortableCollisions(t *testing.T) {
	tests := [][]Entry{
		{{Path: "README", Kind: KindFile}, {Path: "readme", Kind: KindFile}},
		{{Path: "é.txt", Kind: KindFile}, {Path: "e\u0301.txt", Kind: KindFile}},
		{{Path: "parent", Kind: KindFile}, {Path: "parent/child", Kind: KindFile}},
		{{Path: "parent/child", Kind: KindFile}, {Path: "parent", Kind: KindFile}},
		{{Path: "link/child", Kind: KindFile}, {Path: "link", Kind: KindSymlink}},
		{{Path: "link", Kind: KindSymlink}, {Path: "link/child", Kind: KindFile}},
		{{Path: "Parent", Kind: KindFile}, {Path: "parent/child", Kind: KindFile}},
		{{Path: "é", Kind: KindSymlink}, {Path: "e\u0301/child", Kind: KindFile}},
		{{Path: "same", Kind: KindDirectory}, {Path: "same", Kind: KindFile}},
	}
	for _, entries := range tests {
		if _, err := ValidateEntries(entries); !errors.Is(err, ErrPathCollision) {
			t.Fatalf("ValidateEntries(%+v) error = %v, want collision", entries, err)
		}
	}
}

func BenchmarkValidateEntries200K(b *testing.B) {
	entries := make([]Entry, 200_000)
	for i := range entries {
		entries[i] = Entry{Path: fmt.Sprintf("folder/file-%06d", i), Kind: KindFile}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ValidateEntries(entries); err != nil {
			b.Fatal(err)
		}
	}
}

func TestValidateEntriesAllowsSharedDirectories(t *testing.T) {
	entries := []Entry{
		{Path: "folder/a.txt", Kind: KindFile},
		{Path: "folder/b.txt", Kind: KindFile},
		{Path: "empty", Kind: KindDirectory},
	}
	if _, err := ValidateEntries(entries); err != nil {
		t.Fatal(err)
	}
}

func FuzzNormalize(f *testing.F) {
	for _, seed := range []string{"file.txt", "../escape", `C:\NUL`, "a\x1bb", "e\u0301.txt"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		got, err := Normalize(value, false)
		if err != nil {
			return
		}
		if got == "" || got == "." || strings.Contains(got, "\\") {
			t.Fatalf("accepted non-canonical path %q from %q", got, value)
		}
		again, err := Normalize(got, false)
		if err != nil || again != got {
			t.Fatalf("normalization is not idempotent: %q -> %q, %v", got, again, err)
		}
	})
}

func FuzzValidateEntries(f *testing.F) {
	f.Add("alpha.txt", "beta.txt")
	f.Add("README", "readme")
	f.Add("parent", "parent/child")
	f.Fuzz(func(t *testing.T, first, second string) {
		_, _ = ValidateEntries([]Entry{{Path: first, Kind: KindFile}, {Path: second, Kind: KindFile}})
	})
}
