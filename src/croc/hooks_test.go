package croc

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/schollz/croc/v11/src/models"
)

// hooksTransfer sends files from a temp folder to a receiver that writes to
// outDir, with no os.Chdir, and returns the sender and receiver errors. A name
// ending in "/" is an empty folder.
func hooksTransfer(t *testing.T, files map[string][]byte, outDir string, sender, receiver Options) (sendErr, receiveErr error) {
	t.Helper()
	relay := startTestRelay(t, 4)
	source := t.TempDir()
	var paths []string
	for name, content := range files {
		var err error
		if strings.HasSuffix(name, "/") {
			err = os.MkdirAll(filepath.Join(source, name), 0o755)
		} else {
			err = os.WriteFile(filepath.Join(source, name), content, 0o644)
		}
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, filepath.Join(source, name))
	}
	filesInfo, folders, totalFolders, err := GetFilesInfo(paths, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	secret := fmt.Sprintf("hooks-%d", time.Now().UnixNano())
	for _, o := range []*Options{&sender, &receiver} {
		o.SharedSecret, o.RelayAddress, o.RelayPassword = secret, relay.address, "pass123"
		o.DisableLocal, o.DisableClipboard, o.Curve = true, true, "siec"
	}
	sender.IsSender, sender.Transport, sender.RelayPorts = true, TransportRelay, append([]string(nil), relay.allPorts...)
	receiver.OutDir = outDir
	s, err := newTestClient(sender)
	if err != nil {
		t.Fatal(err)
	}
	r, err := newTestClient(receiver)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancelClients(s, r) })
	results := make(chan error, 2)
	go func() { results <- s.Send(filesInfo, folders, totalFolders) }()
	go func() { results <- r.Receive() }()
	timeout := time.After(20 * time.Second)
	var errs []error
	for range 2 {
		select {
		case err := <-results:
			errs = append(errs, err)
		case <-timeout:
			t.Fatal("transfer timed out")
		}
	}
	// Both errors are reported together: which side fails first is a race.
	return errs[0], errs[1]
}

type progressLog struct {
	mu    sync.Mutex
	snaps []Progress
}

func (l *progressLog) add(p Progress) {
	l.mu.Lock()
	l.snaps = append(l.snaps, p)
	l.mu.Unlock()
}

// check verifies every file ends at its full size and no file's count goes backwards.
func (l *progressLog) check(t *testing.T, files map[string][]byte) {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	last := map[string]int64{}
	for _, p := range l.snaps {
		if int(p.Size) != len(files[p.Name]) || p.Sent > p.Size || p.Sent < last[p.Name] {
			t.Fatalf("inconsistent snapshot %+v after %d bytes", p, last[p.Name])
		}
		last[p.Name] = p.Sent
	}
	for name, content := range files {
		if last[name] != int64(len(content)) {
			t.Errorf("%s reported %d of %d bytes", name, last[name], len(content))
		}
	}
}

func TestHooksReceiveWithoutChdir(t *testing.T) {
	small := map[string][]byte{"big.bin": bytes.Repeat([]byte("x"), 3<<20)}
	bundled := map[string][]byte{}
	for i := range 6 {
		bundled[fmt.Sprintf("small-%d.txt", i)] = bytes.Repeat([]byte{byte('a' + i)}, 100+i)
	}
	for name, files := range map[string]map[string][]byte{"single file": small, "bundle": bundled} {
		t.Run(name, func(t *testing.T) {
			outDir := t.TempDir()
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			var sent, received progressLog
			var question Question
			asked := 0
			sendErr, receiveErr := hooksTransfer(t, files, outDir,
				Options{NoPrompt: true, OnProgress: sent.add},
				Options{NoPrompt: true, OnProgress: received.add, Prompt: func(_ context.Context, q Question) (bool, error) {
					asked++
					question = q
					return true, nil
				}})
			if sendErr != nil || receiveErr != nil {
				t.Fatalf("send error %v, receive error %v", sendErr, receiveErr)
			}
			for n, want := range files {
				got, err := os.ReadFile(filepath.Join(outDir, n))
				if err != nil || !bytes.Equal(got, want) {
					t.Errorf("%s in OutDir: %d bytes, %v; want %d bytes", n, len(got), err, len(want))
				}
				if _, err := os.Stat(filepath.Join(cwd, n)); err == nil {
					t.Errorf("%s was written to the working directory", n)
				}
			}
			if now, _ := os.Getwd(); now != cwd {
				t.Errorf("working directory changed to %s", now)
			}
			sent.check(t, files)
			received.check(t, files)
			if asked != 1 || question.Kind != QuestionAccept || question.Required || len(question.Files) != len(files) {
				t.Errorf("asked %d times, last %+v; want one non-required accept question for %d files", asked, question, len(files))
			}
		})
	}
}

func TestHooksPromptCanRefuse(t *testing.T) {
	files := map[string][]byte{"secret.txt": []byte("payload")}
	outDir := t.TempDir()
	sendErr, receiveErr := hooksTransfer(t, files, outDir,
		Options{NoPrompt: true},
		Options{Prompt: func(context.Context, Question) (bool, error) { return false, nil }})
	if sendErr == nil || receiveErr == nil {
		t.Fatalf("send error %v, receive error %v; want both to fail", sendErr, receiveErr)
	}
	if entries, _ := os.ReadDir(outDir); len(entries) != 0 {
		t.Errorf("refused transfer wrote %v", entries)
	}
}

func TestHooksSenderAskForcesReceiverQuestion(t *testing.T) {
	var mu sync.Mutex
	var questions []Question
	record := func(_ context.Context, q Question) (bool, error) {
		mu.Lock()
		defer mu.Unlock()
		questions = append(questions, q)
		return true, nil
	}
	sendErr, receiveErr := hooksTransfer(t, map[string][]byte{"a.txt": []byte("hello")}, t.TempDir(),
		Options{NoPrompt: true, Ask: true, Prompt: record},
		Options{NoPrompt: true, Prompt: record})
	if sendErr != nil || receiveErr != nil {
		t.Fatalf("send error %v, receive error %v", sendErr, receiveErr)
	}
	kinds := map[QuestionKind]bool{}
	for _, q := range questions {
		if !q.Required || q.Name == "" {
			t.Errorf("question %+v: want Required and a machine ID", q)
		}
		kinds[q.Kind] = true
	}
	if len(questions) != 2 || !kinds[QuestionAccept] || !kinds[QuestionSend] {
		t.Errorf("questions = %+v, want one accept and one send", questions)
	}
}

func TestHooksOverwriteQuestionAndUnchangedFileUseOutDir(t *testing.T) {
	outDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outDir, "same.txt"), []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "changed.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	var overwrites []string
	sendErr, receiveErr := hooksTransfer(t, map[string][]byte{"same.txt": []byte("same"), "changed.txt": []byte("new!")}, outDir,
		Options{NoPrompt: true},
		Options{NoPrompt: true, Prompt: func(_ context.Context, q Question) (bool, error) {
			if q.Kind == QuestionOverwrite {
				overwrites = append(overwrites, q.Name)
				if !q.Required {
					t.Error("overwrite question is not Required: croc asks it under NoPrompt")
				}
			}
			return true, nil
		}})
	if sendErr != nil || receiveErr != nil {
		t.Fatalf("send error %v, receive error %v", sendErr, receiveErr)
	}
	if len(overwrites) != 1 || overwrites[0] != "changed.txt" {
		t.Errorf("overwrite questions = %v, want only changed.txt", overwrites)
	}
	if got, _ := os.ReadFile(filepath.Join(outDir, "changed.txt")); string(got) != "new!" {
		t.Errorf("changed.txt = %q, want the sender's content", got)
	}
}

// The sender offers two empty folders: one is non-empty in OutDir, one is
// empty there. The emptiness check looks in OutDir, so only the first is asked.
func TestHooksEmptyFolderQuestionUsesOutDir(t *testing.T) {
	outDir := t.TempDir()
	for _, dir := range []string{"full", "blank"} {
		if err := os.MkdirAll(filepath.Join(outDir, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outDir, "full", "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	var overwrites []string
	sendErr, receiveErr := hooksTransfer(t, map[string][]byte{"full/": nil, "blank/": nil}, outDir,
		Options{NoPrompt: true},
		Options{NoPrompt: true, Prompt: func(_ context.Context, q Question) (bool, error) {
			if q.Kind == QuestionOverwrite {
				overwrites = append(overwrites, q.Name)
				if !q.Required {
					t.Error("overwrite question is not Required: croc asks it under NoPrompt")
				}
			}
			return q.Kind == QuestionAccept, nil
		}})
	if sendErr != nil || receiveErr != nil {
		t.Fatalf("send error %v, receive error %v", sendErr, receiveErr)
	}
	if len(overwrites) != 1 || overwrites[0] != "full" {
		t.Errorf("overwrite questions = %v, want only full", overwrites)
	}
}

// A same-size file that differs only in zeroed chunks resumes: the receiver
// finds the good chunks in OutDir and the first snapshot reports them as kept.
func TestHooksResumeFindsExistingChunksInOutDir(t *testing.T) {
	const chunk = models.TCP_BUFFER_SIZE / 2
	want := bytes.Repeat([]byte{7}, 4*chunk)
	outDir := t.TempDir()
	partial := append(bytes.Repeat([]byte{7}, 2*chunk), make([]byte, 2*chunk)...)
	if err := os.WriteFile(filepath.Join(outDir, "big.bin"), partial, 0o644); err != nil {
		t.Fatal(err)
	}
	var received progressLog
	sendErr, receiveErr := hooksTransfer(t, map[string][]byte{"big.bin": want}, outDir,
		Options{NoPrompt: true},
		Options{NoPrompt: true, OnProgress: received.add, Prompt: func(context.Context, Question) (bool, error) { return true, nil }})
	if sendErr != nil || receiveErr != nil {
		t.Fatalf("send error %v, receive error %v", sendErr, receiveErr)
	}
	if got, _ := os.ReadFile(filepath.Join(outDir, "big.bin")); !bytes.Equal(got, want) {
		t.Error("big.bin does not match the sender's file")
	}
	if len(received.snaps) == 0 || received.snaps[0].Sent != 2*chunk {
		t.Errorf("first snapshot = %+v, want %d bytes kept", received.snaps, 2*chunk)
	}
}

// The overwrite-or-resume question reads the existing file in OutDir to say how
// much of it is done.
func TestAskReceiveOverwriteReadsProgressFromOutDir(t *testing.T) {
	const chunk = models.TCP_BUFFER_SIZE / 2
	outDir := t.TempDir()
	existing := append(bytes.Repeat([]byte{7}, 3*chunk), make([]byte, chunk)...)
	if err := os.WriteFile(filepath.Join(outDir, "big.bin"), existing, 0o644); err != nil {
		t.Fatal(err)
	}
	stderr, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stderr
	os.Stderr = stderr
	t.Cleanup(func() { os.Stderr = original })

	c := &Client{Options: Options{OutDir: outDir, Prompt: func(context.Context, Question) (bool, error) { return false, nil }}}
	if _, err := c.askReceiveOverwrite(context.Background(), FileInfo{Name: "big.bin", FolderRemote: ".", Size: int64(len(existing))}, true); err != nil {
		t.Fatal(err)
	}
	os.Stderr = original
	out, err := os.ReadFile(stderr.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "(75.0%)") {
		t.Errorf("prompt text %q does not report 75.0%% done", out)
	}
}

// With no OutDir the name must stay as croc built it: it reaches progress
// labels that split on "/", so a Windows path rewrite would change the CLI's output.
func TestReceivePathWithoutOutDirIsUnchanged(t *testing.T) {
	if got := (&Client{}).receivePath("sub/file.bin"); got != "sub/file.bin" {
		t.Errorf("receivePath = %q, want the name unchanged", got)
	}
}
