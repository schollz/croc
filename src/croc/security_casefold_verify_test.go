package croc

import "testing"

func TestNormalizeReceiveFolderRejectsCaseFoldedSensitiveComponents(t *testing.T) {
	tests := []string{
		".ssh",
		".SSH",
		".Ssh",
		".sSh",
		"safe/.SSH/files",
		`safe\.SsH\files`,
		".git",
		"safe/.GIT/hooks",
		"safe/.GnUpG/keys",
	}
	for _, folder := range tests {
		t.Run(folder, func(t *testing.T) {
			if _, err := normalizeReceiveFolder(folder); err == nil {
				t.Fatalf("normalizeReceiveFolder(%q) unexpectedly succeeded", folder)
			}
		})
	}
}

func TestNormalizeReceiveFolderAllowsSSHSubstrings(t *testing.T) {
	tests := map[string]string{
		".ssh-backup": ".ssh-backup",
		"my.ssh":      "my.ssh",
		"safe/ssh":    "safe/ssh",
		".git-backup": ".git-backup",
		"my.git":      "my.git",
	}
	for folder, want := range tests {
		t.Run(folder, func(t *testing.T) {
			got, err := normalizeReceiveFolder(folder)
			if err != nil {
				t.Fatalf("normalizeReceiveFolder(%q) returned error: %v", folder, err)
			}
			if got != want {
				t.Fatalf("normalizeReceiveFolder(%q) = %q, want %q", folder, got, want)
			}
		})
	}
}
