package croc

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReceiveMetadataAllowsGitRepository(t *testing.T) {
	files := []FileInfo{
		{Name: "config", FolderRemote: ".git/"},
		{Name: "post-checkout", FolderRemote: ".git/hooks"},
		{Name: "HEAD", FolderRemote: "11/.git/"},
		{Name: "config", FolderRemote: `other\.GIT`},
		{Name: ".git", FolderRemote: "linked"},
		{Name: "config-link", FolderRemote: ".", Symlink: ".git/config"},
	}
	folders := []FileInfo{
		{FolderRemote: "11/.git/refs/tags/"},
		{FolderRemote: "empty/.GIT/"},
	}
	gotFiles, gotFolders, err := validateReceiveMetadata(files, folders)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{".git", ".git/hooks", "11/.git", "other/.GIT", "linked", "."} {
		if gotFiles[i].FolderRemote != want || gotFiles[i].Name != files[i].Name || gotFiles[i].Symlink != files[i].Symlink {
			t.Fatalf("normalized file %d = %+v, want folder %q with original name and target", i, gotFiles[i], want)
		}
	}
	for i, want := range []string{"11/.git/refs/tags", "empty/.GIT"} {
		if gotFolders[i].FolderRemote != want {
			t.Fatalf("normalized folder %d = %q, want %q", i, gotFolders[i].FolderRemote, want)
		}
	}
}

func TestCrocGitRepository(t *testing.T) {
	relay := startTestRelay(t, 4)
	directory := t.TempDir()
	source := filepath.Join(directory, "source", "11")
	destination := filepath.Join(directory, "received")
	files := map[string]string{
		"README.md":                "working tree\n",
		".git/HEAD":                "ref: refs/heads/main\n",
		".git/config":              "[core]\n\tbare = false\n",
		".git/hooks/post-checkout": "#!/bin/sh\nexit 0\n",
		".git/objects/ab/cdef":     "object contents\n",
		"linked/.git":              "gitdir: ../.git/worktrees/linked\n",
	}
	for name, contents := range files {
		filename := filepath.Join(source, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	emptyFolders := []string{".git/refs/tags", ".git/objects/info", "empty/.git"}
	for _, name := range emptyFolders {
		if err := os.MkdirAll(filepath.Join(source, filepath.FromSlash(name)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	filesInfo, folders, totalFolders, err := GetFilesInfo([]string{source}, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(destination)

	sender, err := newTestClient(Options{
		IsSender:         true,
		SharedSecret:     "git-repository-folder-transfer",
		RelayAddress:     relay.address,
		RelayPorts:       append([]string(nil), relay.allPorts...),
		RelayPassword:    "pass123",
		NoPrompt:         true,
		DisableLocal:     true,
		DisableClipboard: true,
		Curve:            "siec",
		Transport:        TransportRelay,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancelClients(sender) })
	receiver, err := newTestClient(Options{
		SharedSecret:     sender.Options.SharedSecret,
		RelayAddress:     relay.address,
		RelayPassword:    "pass123",
		NoPrompt:         true,
		DisableLocal:     true,
		DisableClipboard: true,
		Curve:            "siec",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancelClients(receiver) })
	results := make(chan error, 2)
	go func() { results <- sender.Send(filesInfo, folders, totalFolders) }()
	go func() { results <- receiver.Receive() }()
	waitForTransferResults(t, results, 2, 20*time.Second, sender, receiver)

	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(destination, "11", filepath.FromSlash(name)))
		if err != nil || string(got) != want {
			t.Fatalf("received %q = %q, %v; want %q", name, got, err, want)
		}
	}
	for _, name := range emptyFolders {
		entries, err := os.ReadDir(filepath.Join(destination, "11", filepath.FromSlash(name)))
		if err != nil || len(entries) != 0 {
			t.Fatalf("received empty folder %q contains %v, %v", name, entries, err)
		}
	}
}
