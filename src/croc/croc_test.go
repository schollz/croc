package croc

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/schollz/croc/v10/src/message"
	"github.com/schollz/croc/v10/src/tcp"
	"github.com/schollz/croc/v10/src/utils"
	log "github.com/schollz/logger"
	"github.com/schollz/peerdiscovery"
	"github.com/stretchr/testify/assert"
)

func init() {
	log.SetLevel("trace")

	go tcp.Run("debug", "127.0.0.1", "8281", "pass123", "8282,8283,8284,8285")
	go tcp.Run("debug", "127.0.0.1", "8282", "pass123")
	go tcp.Run("debug", "127.0.0.1", "8283", "pass123")
	go tcp.Run("debug", "127.0.0.1", "8284", "pass123")
	go tcp.Run("debug", "127.0.0.1", "8285", "pass123")
	time.Sleep(1 * time.Second)
}

func TestDiscoverReceivePeersTimesOut(t *testing.T) {
	oldDiscover := peerDiscover
	oldTimeout := receivePeerDiscoveryTimeout
	oldTimeLimit := receivePeerDiscoveryTimeLimit
	defer func() {
		peerDiscover = oldDiscover
		receivePeerDiscoveryTimeout = oldTimeout
		receivePeerDiscoveryTimeLimit = oldTimeLimit
	}()

	receivePeerDiscoveryTimeout = 10 * time.Millisecond
	receivePeerDiscoveryTimeLimit = time.Hour
	peerDiscover = func(settings ...peerdiscovery.Settings) ([]peerdiscovery.Discovered, error) {
		<-settings[0].StopChan
		return nil, nil
	}

	c := &Client{
		Options: Options{
			MulticastAddress: "239.255.255.250",
		},
		stop: newStop(context.Background()),
	}

	start := time.Now()
	discoveries := c.discoverReceivePeers()

	assert.Empty(t, discoveries)
	assert.Less(t, time.Since(start), 500*time.Millisecond)
}

func TestHostileSymlinkThenSameNameFileOverwritesSymlinkTarget(t *testing.T) {
	tmpDir := t.TempDir()
	receiveDir := filepath.Join(tmpDir, "receive")
	if err := os.MkdirAll(receiveDir, 0o755); err != nil {
		t.Fatalf("mkdir receive dir: %v", err)
	}
	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(receiveDir); err != nil {
		t.Fatalf("chdir receive dir: %v", err)
	}
	defer func() {
		if err := os.Chdir(originalCwd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()

	outsideTarget := filepath.Join(tmpDir, "outside-target.txt")
	original := []byte("original content that should remain\n")
	if err := os.WriteFile(outsideTarget, original, 0o644); err != nil {
		t.Fatalf("write outside target: %v", err)
	}

	attackerPayload := []byte("attacker-controlled\n")
	senderInfo := SenderInfo{
		FilesToTransfer: []FileInfo{
			{
				Name:         "dup",
				FolderRemote: ".",
				Size:         0,
				Symlink:      outsideTarget,
			},
			{
				Name:         "dup",
				FolderRemote: ".",
				Size:         int64(len(attackerPayload)),
				Mode:         0o644,
			},
		},
		SendingText: true,
	}
	payload, err := json.Marshal(senderInfo)
	assert.NoError(t, err)

	client := &Client{
		Options: Options{
			NoPrompt:    true,
			SendingText: true,
		},
		FilesHasFinished: make(map[int]struct{}),
	}

	done, err := client.processMessageFileInfo(message.Message{
		Type:  message.TypeFileInfo,
		Bytes: payload,
	})
	assert.True(t, done)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "symlink target")

	got, err := os.ReadFile(outsideTarget)
	assert.NoError(t, err)
	assert.Equal(t, original, got)

	_, err = os.Lstat(filepath.Join(receiveDir, "dup"))
	assert.True(t, os.IsNotExist(err), "hostile metadata should be rejected before creating dup")
}

func TestHostileEmptyFolderTraversalCreatesOutsideCwd(t *testing.T) {
	tmpDir := t.TempDir()
	receiveDir := filepath.Join(tmpDir, "receive")
	if err := os.MkdirAll(receiveDir, 0o755); err != nil {
		t.Fatalf("mkdir receive dir: %v", err)
	}

	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(receiveDir); err != nil {
		t.Fatalf("chdir receive dir: %v", err)
	}
	defer func() {
		if err := os.Chdir(originalCwd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()

	outsideFolder := filepath.Join(tmpDir, "outside-empty-folder")
	hostileFolder := filepath.Join("..", filepath.Base(outsideFolder))
	senderInfo := SenderInfo{
		FilesToTransfer: []FileInfo{
			{
				Name:         "benign.txt",
				FolderRemote: ".",
				Size:         1,
			},
		},
		EmptyFoldersToTransfer: []FileInfo{
			{
				FolderRemote: hostileFolder,
			},
		},
		TotalNumberFolders: 1,
		SendingText:        true,
	}
	payload, err := json.Marshal(senderInfo)
	assert.NoError(t, err)

	client := &Client{
		Options: Options{
			NoPrompt:    true,
			SendingText: true,
		},
		FilesHasFinished: make(map[int]struct{}),
	}

	done, err := client.processMessageFileInfo(message.Message{
		Type:  message.TypeFileInfo,
		Bytes: payload,
	})
	assert.True(t, done)
	assert.Error(t, err)

	_, err = os.Stat(outsideFolder)
	assert.True(t, os.IsNotExist(err), "empty folder metadata should be rejected before creating directories outside the receive directory")
}

func TestHostileRegularFileTraversalRejected(t *testing.T) {
	senderInfo := SenderInfo{
		FilesToTransfer: []FileInfo{
			{
				Name:         "x.txt",
				FolderRemote: filepath.Join("..", "escaped-file"),
				Size:         1,
			},
		},
	}
	payload, err := json.Marshal(senderInfo)
	assert.NoError(t, err)

	client := &Client{
		Options: Options{
			NoPrompt:    true,
			SendingText: true,
		},
		FilesHasFinished: make(map[int]struct{}),
	}

	done, err := client.processMessageFileInfo(message.Message{
		Type:  message.TypeFileInfo,
		Bytes: payload,
	})
	assert.True(t, done)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "filename must be a local path")
}

func TestHostileDuplicateDestinationRejected(t *testing.T) {
	senderInfo := SenderInfo{
		FilesToTransfer: []FileInfo{
			{
				Name:         "dup",
				FolderRemote: ".",
				Size:         1,
			},
			{
				Name:         "dup",
				FolderRemote: "./",
				Size:         2,
			},
		},
		SendingText: true,
	}
	payload, err := json.Marshal(senderInfo)
	assert.NoError(t, err)

	client := &Client{
		Options: Options{
			NoPrompt:    true,
			SendingText: true,
		},
		FilesHasFinished: make(map[int]struct{}),
	}

	done, err := client.processMessageFileInfo(message.Message{
		Type:  message.TypeFileInfo,
		Bytes: payload,
	})
	assert.True(t, done)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate destination path")
}

func TestHostileExistingSymlinkDestinationRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on some Windows setups")
	}

	tmpDir := t.TempDir()
	receiveDir := filepath.Join(tmpDir, "receive")
	if err := os.MkdirAll(receiveDir, 0o755); err != nil {
		t.Fatalf("mkdir receive dir: %v", err)
	}

	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(receiveDir); err != nil {
		t.Fatalf("chdir receive dir: %v", err)
	}
	defer func() {
		if err := os.Chdir(originalCwd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()

	outsideTarget := filepath.Join(tmpDir, "outside-target.txt")
	original := []byte("original content that should remain\n")
	if err := os.WriteFile(outsideTarget, original, 0o644); err != nil {
		t.Fatalf("write outside target: %v", err)
	}
	if err := os.Symlink(outsideTarget, "dup"); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	client := &Client{
		Options: Options{
			SendingText: true,
			Overwrite:   true,
		},
		FilesHasFinished: make(map[int]struct{}),
		FilesToTransfer: []FileInfo{
			{
				Name:         "dup",
				FolderRemote: ".",
				Size:         1,
				Mode:         0o644,
			},
		},
	}

	err = client.recipientInitializeFile()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to open symlink destination")

	got, err := os.ReadFile(outsideTarget)
	assert.NoError(t, err)
	assert.Equal(t, original, got)
}

func TestCrocReadme(t *testing.T) {
	defer os.Remove("README.md")

	log.Debug("setting up sender")
	sender, err := New(Options{
		IsSender:      true,
		SharedSecret:  "8123-testingthecroc",
		Debug:         true,
		RelayAddress:  "127.0.0.1:8281",
		RelayPorts:    []string{"8281"},
		RelayPassword: "pass123",
		Stdout:        false,
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
		GitIgnore:     false,
	})
	if err != nil {
		panic(err)
	}

	log.Debug("setting up receiver")
	receiver, err := New(Options{
		IsSender:      false,
		SharedSecret:  "8123-testingthecroc",
		Debug:         true,
		RelayAddress:  "127.0.0.1:8281",
		RelayPassword: "pass123",
		Stdout:        false,
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
	})
	if err != nil {
		panic(err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		filesInfo, emptyFolders, totalNumberFolders, errGet := GetFilesInfo([]string{"../../README.md"}, false, false, []string{})
		if errGet != nil {
			t.Errorf("failed to get minimal info: %v", errGet)
		}
		err := sender.Send(filesInfo, emptyFolders, totalNumberFolders)
		if err != nil {
			t.Errorf("send failed: %v", err)
		}
		wg.Done()
	}()
	time.Sleep(100 * time.Millisecond)
	go func() {
		err := receiver.Receive()
		if err != nil {
			t.Errorf("receive failed: %v", err)
		}
		wg.Done()
	}()

	wg.Wait()
}

func TestCrocNonASCIIFileName(t *testing.T) {
	testDir := t.TempDir()
	sourceDir := filepath.Join(testDir, "source")
	receiveDir := filepath.Join(testDir, "receive")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("create source directory: %v", err)
	}
	if err := os.MkdirAll(receiveDir, 0o755); err != nil {
		t.Fatalf("create receive directory: %v", err)
	}

	// The 20-byte progress-label truncation boundary falls in the middle of ä.
	const fileName = "1234567890123456789ä.txt"
	want := bytes.Repeat([]byte("x"), 10_000_001)
	sourcePath := filepath.Join(sourceDir, fileName)
	if err := os.WriteFile(sourcePath, want, 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	stderr, err := os.CreateTemp(testDir, "transfer-stderr-")
	if err != nil {
		t.Fatalf("create stderr capture: %v", err)
	}
	originalStderr := os.Stderr
	os.Stderr = stderr
	t.Cleanup(func() {
		os.Stderr = originalStderr
		if err := stderr.Close(); err != nil {
			t.Errorf("close stderr capture: %v", err)
		}
	})

	filesInfo, emptyFolders, totalNumberFolders, err := GetFilesInfo([]string{sourcePath}, false, false, nil)
	if err != nil {
		t.Fatalf("get source file info: %v", err)
	}

	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(receiveDir); err != nil {
		t.Fatalf("change to receive directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalCwd); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	secret := fmt.Sprintf("non-ascii-filename-%d", time.Now().UnixNano())
	sender, err := New(Options{
		IsSender:      true,
		SharedSecret:  secret,
		RelayAddress:  "127.0.0.1:8281",
		RelayPorts:    []string{"8281"},
		RelayPassword: "pass123",
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
		GitIgnore:     false,
	})
	if err != nil {
		t.Fatalf("create sender: %v", err)
	}
	receiver, err := New(Options{
		IsSender:      false,
		SharedSecret:  secret,
		RelayAddress:  "127.0.0.1:8281",
		RelayPassword: "pass123",
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
	})
	if err != nil {
		t.Fatalf("create receiver: %v", err)
	}

	errCh := make(chan error, 2)
	go func() {
		errCh <- sender.Send(filesInfo, emptyFolders, totalNumberFolders)
	}()
	time.Sleep(100 * time.Millisecond)
	go func() {
		errCh <- receiver.Receive()
	}()
	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			t.Errorf("transfer failed: %v", err)
		}
	}

	receivedPath := filepath.Join(receiveDir, fileName)
	got, err := os.ReadFile(receivedPath)
	if err != nil {
		t.Fatalf("read received file %q: %v", fileName, err)
	}
	if !bytes.Equal(want, got) {
		t.Errorf("received payload does not match source")
	}

	if err := stderr.Sync(); err != nil {
		t.Fatalf("sync stderr capture: %v", err)
	}
	output, err := os.ReadFile(stderr.Name())
	if err != nil {
		t.Fatalf("read stderr capture: %v", err)
	}
	for offset := 0; offset < len(output); {
		_, size := utf8.DecodeRune(output[offset:])
		if size == 1 && output[offset] >= utf8.RuneSelf {
			end := min(offset+8, len(output))
			t.Errorf("transfer output is not valid UTF-8 at byte %d near %x", offset, output[offset:end])
			break
		}
		offset += size
	}
}

func TestCrocEmptyFolder(t *testing.T) {
	pathName := "../../testEmpty"
	defer os.RemoveAll(pathName)
	defer os.RemoveAll("./testEmpty")
	os.MkdirAll(pathName, 0o755)

	log.Debug("setting up sender")
	sender, err := New(Options{
		IsSender:      true,
		SharedSecret:  "8123-testingthecroc",
		Debug:         true,
		RelayAddress:  "127.0.0.1:8281",
		RelayPorts:    []string{"8281"},
		RelayPassword: "pass123",
		Stdout:        false,
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
	})
	if err != nil {
		panic(err)
	}

	log.Debug("setting up receiver")
	receiver, err := New(Options{
		IsSender:      false,
		SharedSecret:  "8123-testingthecroc",
		Debug:         true,
		RelayAddress:  "127.0.0.1:8281",
		RelayPassword: "pass123",
		Stdout:        false,
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
	})
	if err != nil {
		panic(err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		filesInfo, emptyFolders, totalNumberFolders, errGet := GetFilesInfo([]string{pathName}, false, false, []string{})
		if errGet != nil {
			t.Errorf("failed to get minimal info: %v", errGet)
		}
		err := sender.Send(filesInfo, emptyFolders, totalNumberFolders)
		if err != nil {
			t.Errorf("send failed: %v", err)
		}
		wg.Done()
	}()
	time.Sleep(100 * time.Millisecond)
	go func() {
		err := receiver.Receive()
		if err != nil {
			t.Errorf("receive failed: %v", err)
		}
		wg.Done()
	}()

	wg.Wait()
}

func TestCrocSymlink(t *testing.T) {
	pathName := "../link-in-folder"
	defer os.RemoveAll(pathName)
	defer os.RemoveAll("./link-in-folder")
	os.MkdirAll(pathName, 0o755)
	if err := os.WriteFile(filepath.Join(pathName, "target.txt"), []byte("safe symlink target"), 0o644); err != nil {
		t.Fatalf("write symlink target: %v", err)
	}
	if err := os.Symlink("target.txt", filepath.Join(pathName, "README.link")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	log.Debug("setting up sender")
	sender, err := New(Options{
		IsSender:      true,
		SharedSecret:  "8124-testingthecroc",
		Debug:         true,
		RelayAddress:  "127.0.0.1:8281",
		RelayPorts:    []string{"8281"},
		RelayPassword: "pass123",
		Stdout:        false,
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
		GitIgnore:     false,
	})
	if err != nil {
		panic(err)
	}

	log.Debug("setting up receiver")
	receiver, err := New(Options{
		IsSender:      false,
		SharedSecret:  "8124-testingthecroc",
		Debug:         true,
		RelayAddress:  "127.0.0.1:8281",
		RelayPassword: "pass123",
		Stdout:        false,
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
	})
	if err != nil {
		panic(err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		filesInfo, emptyFolders, totalNumberFolders, errGet := GetFilesInfo([]string{pathName}, false, false, []string{})
		if errGet != nil {
			t.Errorf("failed to get minimal info: %v", errGet)
		}
		err = sender.Send(filesInfo, emptyFolders, totalNumberFolders)
		if err != nil {
			t.Errorf("send failed: %v", err)
		}
		wg.Done()
	}()
	time.Sleep(100 * time.Millisecond)
	go func() {
		err = receiver.Receive()
		if err != nil {
			t.Errorf("receive failed: %v", err)
		}
		wg.Done()
	}()

	wg.Wait()

	linkPath := filepath.Join("link-in-folder", "README.link")
	s, err := os.Readlink(linkPath)
	if s != "target.txt" {
		t.Errorf("symlink target = %q, want target.txt", s)
	}
	if err != nil {
		t.Errorf("symlink transfer failed: %s", err.Error())
	}
	resolvedLink, err := filepath.EvalSymlinks(linkPath)
	if err != nil {
		t.Errorf("symlink resolution failed: %s", err.Error())
	}
	resolvedTarget, err := filepath.EvalSymlinks(filepath.Join("link-in-folder", "target.txt"))
	if err != nil {
		t.Errorf("target resolution failed: %s", err.Error())
	}
	assert.Equal(t, resolvedTarget, resolvedLink)
}
func TestCrocIgnoreGit(t *testing.T) {
	log.SetLevel("trace")
	defer os.Remove(".gitignore")
	time.Sleep(300 * time.Millisecond)

	time.Sleep(1 * time.Second)
	file, err := os.Create(".gitignore")
	if err != nil {
		log.Errorf("error creating file")
	}
	_, err = file.WriteString("LICENSE")
	if err != nil {
		log.Errorf("error writing to file")
	}
	time.Sleep(1 * time.Second)
	// due to how files are ignored in this function, all we have to do to test is make sure LICENSE doesn't get included in FilesInfo.
	filesInfo, _, _, errGet := GetFilesInfo([]string{"../../LICENSE", ".gitignore", "croc.go"}, false, true, []string{})
	if errGet != nil {
		t.Errorf("failed to get minimal info: %v", errGet)
	}
	for _, file := range filesInfo {
		if strings.Contains(file.Name, "LICENSE") {
			t.Errorf("test failed, should ignore LICENSE")
		}
	}
}

// TestGetFilesInfoZipFolderHonoursFilters covers the integration between
// GetFilesInfo and ZipDirectory when zipfolder=true. Regression test for
// https://github.com/schollz/croc/issues/1087: --exclude and --git were
// silently ignored in zip mode because the args weren't passed through.
func TestGetFilesInfoZipFolderHonoursFilters(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "myproj")
	for _, rel := range []string{
		"main.go",
		"node_modules/pkg.json",
		".venv/file.py",
		"build/output.bin",
	} {
		p := filepath.Join(src, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	// gitWalk calls MatchesPath(info.Name()) on basenames, which doesn't
	// match patterns with trailing slashes. Use a plain pattern.
	gitignore := filepath.Join(src, ".gitignore")
	if err := os.WriteFile(gitignore, []byte("build\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}

	// GetFilesInfo creates the zip in cwd, so chdir to a clean tmp location.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	filesInfo, _, _, err := GetFilesInfo([]string{"myproj"}, true /*zipfolder*/, true /*ignoreGit*/, []string{"node_modules", ".venv"})
	if err != nil {
		t.Fatalf("GetFilesInfo: %v", err)
	}
	if len(filesInfo) != 1 {
		t.Fatalf("expected 1 FileInfo (the zip), got %d", len(filesInfo))
	}
	zipName := filesInfo[0].Name
	defer os.Remove(zipName)

	archive, err := zip.OpenReader(zipName)
	if err != nil {
		t.Fatalf("open zip %s: %v", zipName, err)
	}
	defer archive.Close()

	got := make([]string, 0, len(archive.File))
	for _, f := range archive.File {
		got = append(got, f.Name)
	}
	for _, name := range got {
		if strings.Contains(name, "node_modules") || strings.Contains(name, ".venv") {
			t.Errorf("--exclude leak: %q is in zip\n  members: %v", name, got)
		}
		if strings.Contains(name, "build") {
			t.Errorf("--git leak: %q is in zip\n  members: %v", name, got)
		}
	}
	wantKept := "myproj/main.go"
	found := false
	for _, name := range got {
		if name == wantKept {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected %q in zip; got %v", wantKept, got)
	}
}

func TestGetFilesInfoExactFileExclusion(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	for _, rel := range []string{"a/image.jpg", "b/a/image.jpg", "photo.png"} {
		file := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(file, []byte(rel), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	files, _, _, err := GetFilesInfoWithExactExclusions([]string{root}, false, false, nil, []string{"a/image.jpg"})
	if err != nil {
		t.Fatalf("GetFilesInfoWithExactExclusions: %v", err)
	}
	got := make(map[string]bool)
	for _, file := range files {
		rel, err := filepath.Rel(root, filepath.Join(file.FolderSource, file.Name))
		if err != nil {
			t.Fatalf("relative path: %v", err)
		}
		got[filepath.ToSlash(rel)] = true
	}
	if got["a/image.jpg"] {
		t.Fatal("exactly excluded file was returned")
	}
	for _, want := range []string{"b/a/image.jpg", "photo.png"} {
		if !got[want] {
			t.Errorf("expected %q to be returned; got %v", want, got)
		}
	}
}

func TestGetFilesInfoZipFolderFromInsideSourceExcludesArchiveItself(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "payload")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "file.txt"), []byte("contents"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(src); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	done := make(chan []FileInfo, 1)
	errc := make(chan error, 1)
	go func() {
		filesInfo, _, _, err := GetFilesInfo([]string{src}, true, false, nil)
		if err != nil {
			errc <- err
			return
		}
		done <- filesInfo
	}()

	var filesInfo []FileInfo
	select {
	case filesInfo = <-done:
	case err := <-errc:
		t.Fatalf("GetFilesInfo: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("GetFilesInfo did not return when zipping a directory from inside itself")
	}
	if len(filesInfo) != 1 {
		t.Fatalf("expected 1 FileInfo (the zip), got %d", len(filesInfo))
	}

	archive, err := zip.OpenReader(filesInfo[0].Name)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer archive.Close()

	members := make([]string, 0, len(archive.File))
	foundPayload := false
	for _, f := range archive.File {
		members = append(members, f.Name)
		if f.Name == "payload/payload.zip" {
			t.Fatalf("archive includes itself: %v", members)
		}
		if f.Name == "payload/file.txt" {
			foundPayload = true
		}
	}
	if !foundPayload {
		t.Fatalf("archive is missing payload file: %v", members)
	}
}

func TestCrocLocal(t *testing.T) {
	log.SetLevel("trace")
	defer os.Remove("LICENSE")
	defer os.Remove("touched")
	time.Sleep(300 * time.Millisecond)

	log.Debug("setting up sender")
	sender, err := New(Options{
		IsSender:      true,
		SharedSecret:  "8123-testingthecroc",
		Debug:         true,
		RelayAddress:  "127.0.0.1:8181",
		RelayPorts:    []string{"8181", "8182"},
		RelayPassword: "pass123",
		Stdout:        true,
		NoPrompt:      true,
		DisableLocal:  false,
		Curve:         "ed25519",
		Overwrite:     true,
		GitIgnore:     false,
	})
	if err != nil {
		panic(err)
	}
	time.Sleep(1 * time.Second)

	log.Debug("setting up receiver")
	receiver, err := New(Options{
		IsSender:      false,
		SharedSecret:  "8123-testingthecroc",
		Debug:         true,
		RelayAddress:  "127.0.0.1:8181",
		RelayPassword: "pass123",
		Stdout:        true,
		NoPrompt:      true,
		DisableLocal:  false,
		Curve:         "ed25519",
		Overwrite:     true,
	})
	if err != nil {
		panic(err)
	}

	var wg sync.WaitGroup
	os.Create("touched")
	wg.Add(2)
	go func() {
		filesInfo, emptyFolders, totalNumberFolders, errGet := GetFilesInfo([]string{"../../LICENSE", "touched"}, false, false, []string{})
		if errGet != nil {
			t.Errorf("failed to get minimal info: %v", errGet)
		}
		err := sender.Send(filesInfo, emptyFolders, totalNumberFolders)
		if err != nil {
			t.Errorf("send failed: %v", err)
		}
		wg.Done()
	}()
	time.Sleep(100 * time.Millisecond)
	go func() {
		err := receiver.Receive()
		if err != nil {
			t.Errorf("send failed: %v", err)
		}
		wg.Done()
	}()

	wg.Wait()
}

func TestSenderWaitsForLocalRelayAfterExternalRelayCloses(t *testing.T) {
	log.SetLevel("warn")
	localIPs, err := utils.GetLocalIPs()
	if err != nil || len(localIPs) == 0 {
		t.Skipf("local relay regression requires a non-loopback local IP: %v", err)
	}
	externalPorts := freeConsecutiveTestPorts(t, 5)
	ctx, stopRelay := context.WithCancel(context.Background())
	defer stopRelay()
	go tcp.RunCtx(ctx, "warn", "127.0.0.1", externalPorts[0], "pass123", strings.Join(externalPorts[1:], ","))
	for _, port := range externalPorts[1:] {
		go tcp.RunCtx(ctx, "warn", "127.0.0.1", port, "pass123")
	}
	time.Sleep(250 * time.Millisecond)

	tempFile, cleanup := createTestFile(t, 64)
	defer cleanup()
	receivedFile := filepath.Base(tempFile)
	defer os.Remove(receivedFile)

	secret := fmt.Sprintf("localrelay-%d", time.Now().UnixNano())
	sender, err := New(Options{
		IsSender:      true,
		SharedSecret:  secret,
		Debug:         true,
		RelayAddress:  net.JoinHostPort("127.0.0.1", externalPorts[0]),
		RelayPorts:    append([]string(nil), externalPorts...),
		RelayPassword: "pass123",
		NoPrompt:      true,
		DisableLocal:  false,
		Curve:         "siec",
		Overwrite:     true,
		GitIgnore:     false,
		NoCompress:    true,
	})
	if err != nil {
		t.Fatalf("create sender: %v", err)
	}
	filesInfo, emptyFolders, totalNumberFolders, err := GetFilesInfo([]string{tempFile}, false, false, []string{})
	if err != nil {
		t.Fatalf("GetFilesInfo: %v", err)
	}
	receiver, err := New(Options{
		IsSender:      false,
		SharedSecret:  secret,
		Debug:         true,
		RelayAddress:  net.JoinHostPort("127.0.0.1", externalPorts[0]),
		RelayPassword: "pass123",
		NoPrompt:      true,
		DisableLocal:  false,
		TestFlag:      true,
		Curve:         "siec",
		Overwrite:     true,
		NoCompress:    true,
	})
	if err != nil {
		t.Fatalf("create receiver: %v", err)
	}

	errc := make(chan error, 2)
	go func() {
		errc <- sender.Send(filesInfo, emptyFolders, totalNumberFolders)
	}()
	go func() {
		if err := waitHashed(sender); err != nil {
			errc <- err
			return
		}
		errc <- receiver.Receive()
	}()

	for i := 0; i < 2; i++ {
		if err := <-errc; err != nil {
			t.Fatalf("transfer failed: %v", err)
		}
	}
}

func TestSenderLocalProbeDoesNotCorruptExternalRoute(t *testing.T) {
	log.SetLevel("warn")
	const externalHost = "127.0.0.2"
	probeListener, err := net.Listen("tcp", net.JoinHostPort(externalHost, "0"))
	if err != nil {
		t.Skipf("%s is not available: %v", externalHost, err)
	}
	probeListener.Close()

	externalPorts := freeConsecutiveTestPortsForHost(t, externalHost, 9)
	localPorts := freeConsecutiveTestPorts(t, 5)
	ctx, stopRelay := context.WithCancel(context.Background())
	defer stopRelay()
	go tcp.RunCtx(ctx, "warn", externalHost, externalPorts[0], "pass123", strings.Join(externalPorts[1:], ","))
	for _, port := range externalPorts[1:] {
		go tcp.RunCtx(ctx, "warn", externalHost, port, "pass123")
	}
	time.Sleep(250 * time.Millisecond)

	tempFile, cleanup := createTestFile(t, 0)
	defer cleanup()
	receivedFile := filepath.Base(tempFile)
	defer os.Remove(receivedFile)

	secret := fmt.Sprintf("externalroute-%d", time.Now().UnixNano())
	externalAddress := net.JoinHostPort(externalHost, externalPorts[0])
	sender, err := New(Options{
		IsSender:      true,
		SharedSecret:  secret,
		Debug:         true,
		RelayAddress:  externalAddress,
		RelayPorts:    localPorts,
		RelayPassword: "pass123",
		NoPrompt:      true,
		DisableLocal:  false,
		Curve:         "siec",
		Overwrite:     true,
		GitIgnore:     false,
		NoCompress:    true,
	})
	if err != nil {
		t.Fatalf("create sender: %v", err)
	}
	filesInfo, emptyFolders, totalNumberFolders, err := GetFilesInfo([]string{tempFile}, false, false, []string{})
	if err != nil {
		t.Fatalf("GetFilesInfo: %v", err)
	}
	receiver, err := New(Options{
		IsSender:      false,
		SharedSecret:  secret,
		Debug:         true,
		RelayAddress:  externalAddress,
		RelayPassword: "pass123",
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
		NoCompress:    true,
	})
	if err != nil {
		t.Fatalf("create receiver: %v", err)
	}

	errc := make(chan error, 2)
	go func() {
		errc <- sender.Send(filesInfo, emptyFolders, totalNumberFolders)
	}()
	if err := waitHashed(sender); err != nil {
		t.Fatal(err)
	}
	time.Sleep(800 * time.Millisecond)
	go func() {
		errc <- receiver.Receive()
	}()

	for i := 0; i < 2; i++ {
		select {
		case err := <-errc:
			if err != nil {
				t.Fatalf("transfer failed: %v", err)
			}
		case <-time.After(20 * time.Second):
			t.Fatal("transfer timed out")
		}
	}
	assert.Equal(t, externalAddress, sender.Options.RelayAddress)
	info, err := os.Stat(receivedFile)
	if err != nil {
		t.Fatalf("received file missing: %v", err)
	}
	assert.Equal(t, int64(0), info.Size())
}

func TestCrocError(t *testing.T) {
	content := []byte("temporary file's content")
	tmpfile, err := os.CreateTemp("", "example")
	if err != nil {
		panic(err)
	}

	defer os.Remove(tmpfile.Name()) // clean up

	if _, err = tmpfile.Write(content); err != nil {
		panic(err)
	}
	if err = tmpfile.Close(); err != nil {
		panic(err)
	}

	Debug(false)
	log.SetLevel("warn")
	sender, _ := New(Options{
		IsSender:      true,
		SharedSecret:  "8123-testingthecroc2",
		Debug:         true,
		RelayAddress:  "doesntexistok.com:8381",
		RelayPorts:    []string{"8381", "8382"},
		RelayPassword: "pass123",
		Stdout:        true,
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
	})
	filesInfo, emptyFolders, totalNumberFolders, errGet := GetFilesInfo([]string{tmpfile.Name()}, false, false, []string{})
	if errGet != nil {
		t.Errorf("failed to get minimal info: %v", errGet)
	}
	err = sender.Send(filesInfo, emptyFolders, totalNumberFolders)
	log.Debug(err)
	assert.NotNil(t, err)
}

func TestReceiverStdoutWithInvalidSecret(t *testing.T) {
	// Test for issue: panic when receiving with --stdout and invalid CROC_SECRET
	// This should fail gracefully without panicking
	log.SetLevel("warn")
	unusedPort := freeTestPort(t)
	receiver, err := New(Options{
		IsSender:      false,
		SharedSecret:  "invalid-secret-12345",
		Debug:         true,
		RelayAddress:  net.JoinHostPort("127.0.0.1", unusedPort),
		RelayPassword: "pass123",
		Stdout:        true, // This is the key flag that triggered the panic
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
	})
	if err != nil {
		t.Errorf("failed to create receiver: %v", err)
		return
	}

	// This should fail but not panic
	err = receiver.Receive()
	// We expect an error since the secret is invalid and no sender is present
	assert.NotNil(t, err)
	log.Debugf("Expected error occurred: %v", err)
}

func TestCleanUp(t *testing.T) {
	// windows allows files to be deleted only if they
	// are not open by another program so the remove actions
	// from the above tests will not always do a good clean up
	// This "test" will make sure
	operatingSystem := runtime.GOOS
	log.Debugf("The operating system is %s", operatingSystem)
	if operatingSystem == "windows" {
		time.Sleep(1 * time.Second)
		log.Debug("Full cleanup")
		var err error

		for _, file := range []string{"README.md", "./README.md"} {
			err = os.Remove(file)
			if err == nil {
				log.Debugf("Successfully purged %s", file)
			} else {
				log.Debugf("%s was already purged.", file)
			}
		}
		for _, folder := range []string{"./testEmpty", "./link-in-folder"} {
			err = os.RemoveAll(folder)
			if err == nil {
				log.Debugf("Successfully purged %s", folder)
			} else {
				log.Debugf("%s was already purged.", folder)
			}
		}
	}
}

func hashed(c *Client) bool {
	if len(c.FilesToTransfer) == 0 {
		return false
	}
	for _, file := range c.FilesToTransfer {
		if len(file.Hash) == 0 {
			return false
		}
	}
	return true
}

func waitHashed(sender *Client) (err error) {
	err = fmt.Errorf("not hashed")
	for i := 0; i < 300; i++ { // Max 3 seconds
		if hashed(sender) {
			time.Sleep(100 * time.Millisecond)
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return
}

func createTestFile(t *testing.T, size int) (string, func()) {
	tempFile, err := os.CreateTemp("", "test-*.dat")
	if err != nil {
		t.Fatal(err)
	}

	data := make([]byte, size)
	for i := 0; i < size; i++ {
		data[i] = byte(i % 256)
	}

	if _, err := tempFile.Write(data); err != nil {
		tempFile.Close()
		os.Remove(tempFile.Name())
		t.Fatal(err)
	}

	if err := tempFile.Close(); err != nil {
		os.Remove(tempFile.Name())
		t.Fatal(err)
	}

	return tempFile.Name(), func() {
		os.Remove(tempFile.Name())
	}
}

func freeTestPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for free port: %v", err)
	}
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}

func freeConsecutiveTestPorts(t *testing.T, count int) []string {
	t.Helper()
	return freeConsecutiveTestPortsForHost(t, "127.0.0.1", count)
}

func freeConsecutiveTestPortsForHost(t *testing.T, host string, count int) []string {
	t.Helper()
	for attempts := 0; attempts < 100; attempts++ {
		base := 20000 + rand.Intn(20000)
		listeners := make([]net.Listener, 0, count)
		ports := make([]string, 0, count)
		ok := true
		for i := 0; i < count; i++ {
			port := strconv.Itoa(base + i)
			listener, err := net.Listen("tcp", net.JoinHostPort(host, port))
			if err != nil {
				ok = false
				break
			}
			listeners = append(listeners, listener)
			ports = append(ports, port)
		}
		for _, listener := range listeners {
			listener.Close()
		}
		if ok {
			return ports
		}
	}
	t.Fatalf("could not find %d consecutive free ports", count)
	return nil
}

func startReconnectRelay(t *testing.T) (controlPort string, cancel func()) {
	t.Helper()
	controlPort = freeTestPort(t)
	dataPort := freeTestPort(t)
	ctx, stop := context.WithCancel(context.Background())
	go tcp.RunCtx(ctx, "warn", "127.0.0.1", controlPort, "pass123", dataPort)
	go tcp.RunCtx(ctx, "warn", "127.0.0.1", dataPort, "pass123")
	time.Sleep(250 * time.Millisecond)
	return controlPort, stop
}

func waitForReconnectCondition(timeout time.Duration, condition func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func runReconnectDropTest(t *testing.T, connIndex int, disableReceiverReconnect bool) error {
	t.Helper()
	tempFile, cleanup := createTestFile(t, 2*1024*1024)
	defer cleanup()
	receivedFile := filepath.Base(tempFile)
	defer os.Remove(receivedFile)

	controlPort, stopRelay := startReconnectRelay(t)
	defer stopRelay()

	uniqueSecret := fmt.Sprintf("reconnect-%d-%d", time.Now().UnixNano(), rand.Intn(10000))
	sender, err := New(Options{
		IsSender:       true,
		SharedSecret:   uniqueSecret,
		Debug:          true,
		RelayAddress:   "127.0.0.1:" + controlPort,
		RelayPassword:  "pass123",
		NoPrompt:       true,
		DisableLocal:   true,
		Curve:          "siec",
		Overwrite:      true,
		GitIgnore:      false,
		NoCompress:     true,
		NoMultiplexing: true,
		ThrottleUpload: "512K",
	})
	if err != nil {
		t.Fatalf("Create sender failed: %v", err)
	}

	filesInfo, emptyFolders, totalNumberFolders, errGet := GetFilesInfo([]string{tempFile}, false, false, []string{})
	if errGet != nil {
		t.Fatalf("Get file info failed: %v", errGet)
	}

	receiver, err := New(Options{
		IsSender:       false,
		SharedSecret:   uniqueSecret,
		Debug:          true,
		RelayAddress:   "127.0.0.1:" + controlPort,
		RelayPassword:  "pass123",
		NoPrompt:       true,
		DisableLocal:   true,
		Curve:          "siec",
		Overwrite:      true,
		NoCompress:     true,
		NoMultiplexing: true,
	})
	if err != nil {
		t.Fatalf("Create receiver failed: %v", err)
	}
	if disableReceiverReconnect {
		receiver.reconnectVersion = 0
	}
	deterministicReconnectRoom := sender.baseRoomName + "-reconnect-1"

	errc := make(chan error, 2)
	go func() {
		errc <- sender.Send(filesInfo, emptyFolders, totalNumberFolders)
	}()
	go func() {
		if err := waitHashed(sender); err != nil {
			errc <- err
			return
		}
		errc <- receiver.Receive()
	}()

	dropped := make(chan struct{})
	go func() {
		defer close(dropped)
		ok := waitForReconnectCondition(5*time.Second, func() bool {
			return sender.Step4FileTransferred && len(sender.conn) > connIndex && sender.conn[connIndex] != nil
		})
		if !ok {
			return
		}
		time.Sleep(150 * time.Millisecond)
		sender.conn[connIndex].Close()
	}()

	var firstErr error
	for i := 0; i < 2; i++ {
		select {
		case err := <-errc:
			if err != nil && firstErr == nil {
				firstErr = err
			}
		case <-time.After(20 * time.Second):
			t.Fatal("reconnect transfer timed out")
		}
	}
	<-dropped
	if firstErr != nil {
		return firstErr
	}
	assert.NotEqual(t, deterministicReconnectRoom, sender.Options.RoomName)
	assert.NotEqual(t, sender.baseRoomName, sender.Options.RoomName)

	original, err := os.ReadFile(tempFile)
	if err != nil {
		t.Fatalf("read original: %v", err)
	}
	received, err := os.ReadFile(receivedFile)
	if err != nil {
		t.Fatalf("read received: %v", err)
	}
	assert.Equal(t, original, received)
	return nil
}

func TestGenerateReconnectRoom(t *testing.T) {
	const baseRoom = "base-room"

	first, err := generateReconnectRoom()
	assert.NoError(t, err)
	second, err := generateReconnectRoom()
	assert.NoError(t, err)

	assert.NotEmpty(t, first)
	assert.NotEmpty(t, second)
	assert.NotEqual(t, first, second)
	assert.NotContains(t, first, baseRoom)
	assert.NotContains(t, second, baseRoom)
}

func TestReconnectRetryEligibility(t *testing.T) {
	c := &Client{
		reconnectVersion:     ReconnectVersion,
		peerReconnectVersion: ReconnectVersion,
		nextReconnectRoom:    "next-room",
		stop:                 newStop(context.Background()),
	}
	assert.True(t, c.canRetryTransfer(transferDisconnectError{err: fmt.Errorf("EOF")}, 0))
	assert.False(t, c.canRetryTransfer(transferDisconnectError{err: fmt.Errorf("EOF")}, maxReconnectAttempts))
	assert.False(t, c.canRetryTransfer(fmt.Errorf("local file error"), 0))
	c.nextReconnectRoom = ""
	assert.False(t, c.canRetryTransfer(transferDisconnectError{err: fmt.Errorf("EOF")}, 0))
	c.nextReconnectRoom = "next-room"
	c.peerReconnectVersion = 0
	assert.False(t, c.canRetryTransfer(transferDisconnectError{err: fmt.Errorf("EOF")}, 0))
}

func TestReconnectFallsBackToRememberedRelay(t *testing.T) {
	controlPort, stopRelay := startReconnectRelay(t)
	defer stopRelay()
	relayAddress := net.JoinHostPort("127.0.0.1", controlPort)
	deadAddress := net.JoinHostPort("127.0.0.1", freeTestPort(t))
	secret := fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	room := fmt.Sprintf("fallback-room-%d", time.Now().UnixNano())

	sender, err := New(Options{
		IsSender:       true,
		SharedSecret:   secret,
		Debug:          true,
		RelayAddress:   relayAddress,
		RelayPassword:  "pass123",
		NoPrompt:       true,
		DisableLocal:   true,
		Curve:          "siec",
		NoMultiplexing: true,
	})
	if err != nil {
		t.Fatalf("create sender: %v", err)
	}
	receiver, err := New(Options{
		IsSender:       false,
		SharedSecret:   secret,
		Debug:          true,
		RelayAddress:   relayAddress,
		RelayPassword:  "pass123",
		NoPrompt:       true,
		DisableLocal:   true,
		Curve:          "siec",
		NoMultiplexing: true,
	})
	if err != nil {
		t.Fatalf("create receiver: %v", err)
	}

	for _, client := range []*Client{sender, receiver} {
		client.nextReconnectRoom = room
		client.setRelayControlAddress(deadAddress)
		client.rememberReconnectRelayAddress(relayAddress)
	}

	errc := make(chan error, 1)
	go func() {
		errc <- sender.senderReconnectRelayAttempt(1)
	}()
	if err := receiver.receiverReconnectRelayAttempt(1); err != nil {
		t.Fatalf("receiver reconnect: %v", err)
	}
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("sender reconnect: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("sender reconnect timed out")
	}

	assert.Equal(t, relayAddress, sender.relayControlAddress)
	assert.Equal(t, relayAddress, receiver.relayControlAddress)
	assert.Equal(t, relayAddress, sender.Options.RelayAddress)
	assert.Equal(t, relayAddress, receiver.Options.RelayAddress)
}

func TestSenderWaitsPastAlternateRouteTimeoutAfterTransferStarts(t *testing.T) {
	oldTimeout := alternateSenderRouteTimeout
	oldPollInterval := alternateSenderRoutePollInterval
	defer func() {
		alternateSenderRouteTimeout = oldTimeout
		alternateSenderRoutePollInterval = oldPollInterval
	}()
	alternateSenderRouteTimeout = 30 * time.Millisecond
	alternateSenderRoutePollInterval = 5 * time.Millisecond

	c := &Client{
		stop: newStop(context.Background()),
	}
	errchan := make(chan error, 1)
	originalErr := fmt.Errorf("losing route EOF")

	go func() {
		time.Sleep(10 * time.Millisecond)
		c.firstSend = true
		time.Sleep(60 * time.Millisecond)
		errchan <- nil
	}()

	start := time.Now()
	err := c.waitForAlternateSenderRoute(errchan, originalErr)

	assert.NoError(t, err)
	assert.GreaterOrEqual(t, time.Since(start), 50*time.Millisecond)
}

func TestReconnectResumesControlDrop(t *testing.T) {
	err := runReconnectDropTest(t, 0, false)
	assert.NoError(t, err)
}

func TestReconnectResumesDataDrop(t *testing.T) {
	err := runReconnectDropTest(t, 1, false)
	assert.NoError(t, err)
}

func TestReconnectDisabledPeerReturnsCleanError(t *testing.T) {
	err := runReconnectDropTest(t, 1, true)
	assert.Error(t, err)
	assert.NotContains(t, fmt.Sprintf("%v", err), "panic")
}

func TestBase(t *testing.T) {
	tempFile, cleanup := createTestFile(t, 1024*1024) // 1 МБ
	defer cleanup()
	receivedFile := filepath.Base(tempFile)
	defer os.Remove(receivedFile)

	go tcp.Run("debug", "127.0.0.1", "8286", "pass123", "8287")
	time.Sleep(200 * time.Millisecond)
	go tcp.Run("debug", "127.0.0.1", "8287", "pass123")
	time.Sleep(200 * time.Millisecond)

	uniqueSecret := fmt.Sprintf("test-%d-%d", time.Now().UnixNano(), rand.Intn(10000))

	sender, err := New(Options{
		IsSender:      true,
		SharedSecret:  uniqueSecret,
		Debug:         true,
		RelayAddress:  "127.0.0.1:8286",
		RelayPassword: "pass123",
		Stdout:        false,
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
		GitIgnore:     false,
	})
	if err != nil {
		t.Fatalf("Create sender failed: %v", err)
	}

	filesInfo, emptyFolders, totalNumberFolders, errGet := GetFilesInfo([]string{tempFile}, false, false, []string{})
	if errGet != nil {
		t.Fatalf("Get file info failed: %v", errGet)
	}

	receiver, err := New(Options{
		IsSender:      false,
		SharedSecret:  uniqueSecret,
		Debug:         true,
		RelayAddress:  "127.0.0.1:8286",
		RelayPassword: "pass123",
		Stdout:        false,
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
	})
	if err != nil {
		t.Fatalf("Create receiver failed: %v", err)
	}

	fatalErr := make(chan error, 1)

	failTest := func(err error) {
		select {
		case fatalErr <- err:
		default:
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		log.Warn("Send")
		if err := sender.Send(filesInfo, emptyFolders, totalNumberFolders); err != nil {
			failTest(fmt.Errorf("Send failed: %w", err))
		}
	}()

	go func() {
		defer wg.Done()

		if err := waitHashed(sender); err != nil {
			failTest(fmt.Errorf("waitHashed failed: %w", err))
			return
		}

		log.Warn("Receive")
		if err := receiver.Receive(); err != nil {
			failTest(fmt.Errorf("Receive failed: %w", err))
		}
	}()

	go func() {
		for i := 0; i < 3000; i++ {
			if sender.Step1ChannelSecured && receiver.Step1ChannelSecured {
				time.Sleep(time.Millisecond)
				if sender.Step2FileInfoTransferred && receiver.Step2FileInfoTransferred {
					log.Warn("Step2FileInfoTransferred reached")
					return
				}
				log.Warn("Step1ChannelSecured reached")
			}
			time.Sleep(time.Millisecond)
		}
	}()

	done := make(chan bool, 1)
	go func() {
		wg.Wait()
		done <- true
	}()

	select {
	case err := <-fatalErr:
		t.Fatal(err)
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Test timeout after 5 seconds")
	}
}

func TestCtx(t *testing.T) {
	tempFile, cleanup := createTestFile(t, 1024*1024) // 1 МБ
	defer cleanup()
	receivedFile := filepath.Base(tempFile)
	defer os.Remove(receivedFile)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	go tcp.RunCtx(ctx, "debug", "127.0.0.1", "8288", "pass123", "8289")
	time.Sleep(200 * time.Millisecond)
	go tcp.RunCtx(ctx, "debug", "127.0.0.1", "8289", "pass123")
	time.Sleep(200 * time.Millisecond)

	uniqueSecret := fmt.Sprintf("test-%d-%d", time.Now().UnixNano(), rand.Intn(10000))

	sender, err := NewCtx(ctx, Options{
		IsSender:      true,
		SharedSecret:  uniqueSecret,
		Debug:         true,
		RelayAddress:  "127.0.0.1:8288",
		RelayPassword: "pass123",
		Stdout:        false,
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
		GitIgnore:     false,
	})
	if err != nil {
		t.Fatalf("Create sender failed: %v", err)
	}

	filesInfo, emptyFolders, totalNumberFolders, errGet := GetFilesInfo([]string{tempFile}, false, false, []string{})
	if errGet != nil {
		t.Fatalf("Get file info failed: %v", errGet)
	}

	receiver, err := NewCtx(ctx, Options{
		IsSender:      false,
		SharedSecret:  uniqueSecret,
		Debug:         true,
		RelayAddress:  "127.0.0.1:8288",
		RelayPassword: "pass123",
		Stdout:        false,
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
	})
	if err != nil {
		t.Fatalf("Create receiver failed: %v", err)
	}

	fatalErr := make(chan error, 1)

	failTest := func(err error) {
		select {
		case fatalErr <- err:
		default:
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		log.Warn("Send")
		if err := sender.Send(filesInfo, emptyFolders, totalNumberFolders); err != nil {
			failTest(fmt.Errorf("Send failed: %w", err))
		}
	}()

	go func() {
		defer wg.Done()

		if err := waitHashed(sender); err != nil {
			failTest(fmt.Errorf("waitHashed failed: %w", err))
			return
		}

		log.Warn("Receive")
		if err := receiver.Receive(); err != nil {
			failTest(fmt.Errorf("Receive failed: %w", err))
		}
	}()

	go func() {
		for i := 0; i < 3000; i++ {
			if sender.Step1ChannelSecured && receiver.Step1ChannelSecured {
				time.Sleep(time.Millisecond)
				if sender.Step2FileInfoTransferred && receiver.Step2FileInfoTransferred {
					log.Warn("Step2FileInfoTransferred reached")
					return
				}
				log.Warn("Step1ChannelSecured reached")
			}
			time.Sleep(time.Millisecond)
		}
	}()

	done := make(chan bool, 1)
	go func() {
		wg.Wait()
		done <- true
	}()

	select {
	case err := <-fatalErr:
		t.Fatal(err)
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Test timeout after 5 seconds")
	}
}

func validErrors(err error) bool {
	s := err.Error()
	return strings.Contains(s, "cancel") ||
		strings.Contains(s, "context") ||
		strings.Contains(s, "reset") ||
		strings.Contains(s, "broken") ||
		strings.Contains(s, "refusing") ||
		strings.Contains(s, "EOF") ||
		strings.Contains(s, "closed")
}

func result(t *testing.T, err error) {
	if err != nil {
		if validErrors(err) {
			t.Logf("Expected error during context cancellation: %v", err)
		} else {
			t.Errorf("Unexpected error during cancellation: %v", err)
		}
		return
	}
	t.Error("Transfer should have been interrupted by context cancellation")
}

func TestAllCtx(t *testing.T) {
	tempFile, cleanup := createTestFile(t, 1024*1024) // 1 МБ
	defer cleanup()
	receivedFile := filepath.Base(tempFile)
	defer os.Remove(receivedFile)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	go tcp.RunCtx(ctx, "debug", "127.0.0.1", "8290", "pass123", "8291")
	time.Sleep(200 * time.Millisecond)
	go tcp.RunCtx(ctx, "debug", "127.0.0.1", "8291", "pass123")
	time.Sleep(200 * time.Millisecond)

	uniqueSecret := fmt.Sprintf("test-%d-%d", time.Now().UnixNano(), rand.Intn(10000))

	sender, err := NewCtx(ctx, Options{
		IsSender:      true,
		SharedSecret:  uniqueSecret,
		Debug:         true,
		RelayAddress:  "127.0.0.1:8290",
		RelayPassword: "pass123",
		Stdout:        false,
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
		GitIgnore:     false,
	})
	if err != nil {
		t.Fatalf("Create sender failed: %v", err)
	}

	filesInfo, emptyFolders, totalNumberFolders, errGet := GetFilesInfo([]string{tempFile}, false, false, []string{})
	if errGet != nil {
		t.Fatalf("Get file info failed: %v", errGet)
	}

	receiver, err := NewCtx(ctx, Options{
		IsSender:      false,
		SharedSecret:  uniqueSecret,
		Debug:         true,
		RelayAddress:  "127.0.0.1:8290",
		RelayPassword: "pass123",
		Stdout:        false,
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
	})
	if err != nil {
		t.Fatalf("Create receiver failed: %v", err)
	}

	fatalErr := make(chan error, 1)

	failTest := func(err error) {
		select {
		case fatalErr <- err:
		default:
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		log.Warn("Send")
		if err := sender.Send(filesInfo, emptyFolders, totalNumberFolders); err != nil {
			failTest(fmt.Errorf("Send failed: %w", err))
		}
	}()

	go func() {
		defer wg.Done()

		if err := waitHashed(sender); err != nil {
			failTest(fmt.Errorf("waitHashed failed: %w", err))
			return
		}

		log.Warn("Receive")
		if err := receiver.Receive(); err != nil {
			failTest(fmt.Errorf("Receive failed: %w", err))
		}
	}()

	go func() {
		for i := 0; i < 3000; i++ {
			if sender.Step1ChannelSecured && receiver.Step1ChannelSecured {
				time.Sleep(time.Millisecond)
				if sender.Step2FileInfoTransferred && receiver.Step2FileInfoTransferred {
					log.Warn("Step2FileInfoTransferred reached")
					cancel()
					return
				}
				log.Warn("Step1ChannelSecured reached")
			}
			time.Sleep(time.Millisecond)
		}
	}()

	done := make(chan bool, 1)
	go func() {
		wg.Wait()
		done <- true
	}()

	select {
	case err := <-fatalErr:
		result(t, err)
	case <-done:
		t.Error("Transfer should have been interrupted by context cancellation")
	case <-time.After(5 * time.Second):
		t.Fatal("Test timeout after 5 seconds")
	}
}

func TestSendCtx(t *testing.T) {
	tempFile, cleanup := createTestFile(t, 1024*1024) // 1 МБ
	defer cleanup()
	receivedFile := filepath.Base(tempFile)
	defer os.Remove(receivedFile)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	go tcp.RunCtx(ctx, "debug", "127.0.0.1", "8292", "pass123", "8293")
	time.Sleep(200 * time.Millisecond)
	go tcp.RunCtx(ctx, "debug", "127.0.0.1", "8293", "pass123")
	time.Sleep(200 * time.Millisecond)

	uniqueSecret := fmt.Sprintf("test-%d-%d", time.Now().UnixNano(), rand.Intn(10000))

	sender, err := NewCtx(ctx2, Options{
		IsSender:      true,
		SharedSecret:  uniqueSecret,
		Debug:         true,
		RelayAddress:  "127.0.0.1:8292",
		RelayPassword: "pass123",
		Stdout:        false,
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
		GitIgnore:     false,
	})
	if err != nil {
		t.Fatalf("Create sender failed: %v", err)
	}

	filesInfo, emptyFolders, totalNumberFolders, errGet := GetFilesInfo([]string{tempFile}, false, false, []string{})
	if errGet != nil {
		t.Fatalf("Get file info failed: %v", errGet)
	}

	receiver, err := NewCtx(ctx, Options{
		IsSender:      false,
		SharedSecret:  uniqueSecret,
		Debug:         true,
		RelayAddress:  "127.0.0.1:8292",
		RelayPassword: "pass123",
		Stdout:        false,
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
	})
	if err != nil {
		t.Fatalf("Create receiver failed: %v", err)
	}

	fatalErr := make(chan error, 1)

	failTest := func(err error) {
		select {
		case fatalErr <- err:
		default:
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		log.Warn("Send")
		if err := sender.Send(filesInfo, emptyFolders, totalNumberFolders); err != nil {
			failTest(fmt.Errorf("Send failed: %w", err))
		}
	}()

	go func() {
		defer wg.Done()

		if err := waitHashed(sender); err != nil {
			failTest(fmt.Errorf("waitHashed failed: %w", err))
			return
		}

		log.Warn("Receive")
		if err := receiver.Receive(); err != nil {
			failTest(fmt.Errorf("Receive failed: %w", err))
		}
	}()

	go func() {
		for i := 0; i < 3000; i++ {
			if sender.Step1ChannelSecured && receiver.Step1ChannelSecured {
				time.Sleep(time.Millisecond)
				if sender.Step2FileInfoTransferred && receiver.Step2FileInfoTransferred {
					log.Warn("Step2FileInfoTransferred reached")
					cancel2()
					return
				}
				log.Warn("Step1ChannelSecured reached")
			}
			time.Sleep(time.Millisecond)
		}
	}()

	done := make(chan bool, 1)
	go func() {
		wg.Wait()
		done <- true
	}()

	select {
	case err := <-fatalErr:
		result(t, err)
	case <-done:
		t.Error("Transfer should have been interrupted by context cancellation")
	case <-time.After(5 * time.Second):
		t.Fatal("Test timeout after 5 seconds")
	}
}

func TestReceiveCtx(t *testing.T) {
	tempFile, cleanup := createTestFile(t, 1024*1024) // 1 МБ
	defer cleanup()
	receivedFile := filepath.Base(tempFile)
	defer os.Remove(receivedFile)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	go tcp.RunCtx(ctx, "debug", "127.0.0.1", "8294", "pass123", "8295")
	time.Sleep(200 * time.Millisecond)
	go tcp.RunCtx(ctx, "debug", "127.0.0.1", "8295", "pass123")
	time.Sleep(200 * time.Millisecond)

	uniqueSecret := fmt.Sprintf("test-%d-%d", time.Now().UnixNano(), rand.Intn(10000))

	sender, err := NewCtx(ctx, Options{
		IsSender:      true,
		SharedSecret:  uniqueSecret,
		Debug:         true,
		RelayAddress:  "127.0.0.1:8294",
		RelayPassword: "pass123",
		Stdout:        false,
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
		GitIgnore:     false,
	})
	if err != nil {
		t.Fatalf("Create sender failed: %v", err)
	}

	filesInfo, emptyFolders, totalNumberFolders, errGet := GetFilesInfo([]string{tempFile}, false, false, []string{})
	if errGet != nil {
		t.Fatalf("Get file info failed: %v", errGet)
	}

	receiver, err := NewCtx(ctx2, Options{
		IsSender:      false,
		SharedSecret:  uniqueSecret,
		Debug:         true,
		RelayAddress:  "127.0.0.1:8294",
		RelayPassword: "pass123",
		Stdout:        false,
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
	})
	if err != nil {
		t.Fatalf("Create receiver failed: %v", err)
	}

	fatalErr := make(chan error, 1)

	failTest := func(err error) {
		select {
		case fatalErr <- err:
		default:
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		log.Warn("Send")
		if err := sender.Send(filesInfo, emptyFolders, totalNumberFolders); err != nil {
			failTest(fmt.Errorf("Send failed: %w", err))
		}
	}()

	go func() {
		defer wg.Done()

		if err := waitHashed(sender); err != nil {
			failTest(fmt.Errorf("waitHashed failed: %w", err))
			return
		}

		log.Warn("Receive")
		if err := receiver.Receive(); err != nil {
			failTest(fmt.Errorf("Receive failed: %w", err))
		}
	}()

	go func() {
		for i := 0; i < 3000; i++ {
			if sender.Step1ChannelSecured && receiver.Step1ChannelSecured {
				time.Sleep(time.Millisecond)
				if sender.Step2FileInfoTransferred && receiver.Step2FileInfoTransferred {
					log.Warn("Step2FileInfoTransferred reached")
					cancel2()
					return
				}
				log.Warn("Step1ChannelSecured reached")
			}
			time.Sleep(time.Millisecond)
		}
	}()

	done := make(chan bool, 1)
	go func() {
		wg.Wait()
		done <- true
	}()

	select {
	case err := <-fatalErr:
		result(t, err)
	case <-done:
		t.Error("Transfer should have been interrupted by context cancellation")
	case <-time.After(5 * time.Second):
		t.Fatal("Test timeout after 5 seconds")
	}
}

func TestRunCtx(t *testing.T) {
	tempFile, cleanup := createTestFile(t, 1024*1024) // 1 МБ
	defer cleanup()
	receivedFile := filepath.Base(tempFile)
	defer os.Remove(receivedFile)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	go tcp.RunCtx(ctx2, "debug", "127.0.0.1", "8296", "pass123", "8297")
	time.Sleep(200 * time.Millisecond)
	go tcp.RunCtx(ctx2, "debug", "127.0.0.1", "8297", "pass123")
	time.Sleep(200 * time.Millisecond)

	uniqueSecret := fmt.Sprintf("test-%d-%d", time.Now().UnixNano(), rand.Intn(10000))

	sender, err := NewCtx(ctx, Options{
		IsSender:      true,
		SharedSecret:  uniqueSecret,
		Debug:         true,
		RelayAddress:  "127.0.0.1:8296",
		RelayPassword: "pass123",
		Stdout:        false,
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
		GitIgnore:     false,
	})
	if err != nil {
		t.Fatalf("Create sender failed: %v", err)
	}

	filesInfo, emptyFolders, totalNumberFolders, errGet := GetFilesInfo([]string{tempFile}, false, false, []string{})
	if errGet != nil {
		t.Fatalf("Get file info failed: %v", errGet)
	}

	receiver, err := NewCtx(ctx, Options{
		IsSender:      false,
		SharedSecret:  uniqueSecret,
		Debug:         true,
		RelayAddress:  "127.0.0.1:8296",
		RelayPassword: "pass123",
		Stdout:        false,
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
	})
	if err != nil {
		t.Fatalf("Create receiver failed: %v", err)
	}

	fatalErr := make(chan error, 1)

	failTest := func(err error) {
		select {
		case fatalErr <- err:
		default:
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		log.Warn("Send")
		if err := sender.Send(filesInfo, emptyFolders, totalNumberFolders); err != nil {
			failTest(fmt.Errorf("Send failed: %w", err))
		}
	}()

	go func() {
		defer wg.Done()

		if err := waitHashed(sender); err != nil {
			failTest(fmt.Errorf("waitHashed failed: %w", err))
			return
		}

		log.Warn("Receive")
		if err := receiver.Receive(); err != nil {
			failTest(fmt.Errorf("Receive failed: %w", err))
		}
	}()

	go func() {
		for i := 0; i < 3000; i++ {
			if sender.Step1ChannelSecured && receiver.Step1ChannelSecured {
				time.Sleep(time.Millisecond)
				if sender.Step2FileInfoTransferred && receiver.Step2FileInfoTransferred {
					log.Warn("Step2FileInfoTransferred reached")
					cancel2()
					return
				}
				log.Warn("Step1ChannelSecured reached")
			}
			time.Sleep(time.Millisecond)
		}
	}()

	done := make(chan bool, 1)
	go func() {
		wg.Wait()
		done <- true
	}()

	select {
	case err := <-fatalErr:
		result(t, err)
	case <-done:
		t.Error("Transfer should have been interrupted by context cancellation")
	case <-time.After(5 * time.Second):
		t.Fatal("Test timeout after 5 seconds")
	}
}
