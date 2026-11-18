package croc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// promptTransfer sends files from a temp folder to a receiver in another temp
// folder (the working directory for the test) and returns the receive folder
// and both errors. A name ending in "/" is an empty folder; existing holds
// files to create in the receive folder first.
func promptTransfer(t *testing.T, files, existing map[string]string, sender, receiver Options) (receiveDir string, sendErr, receiveErr error) {
	t.Helper()
	relay := startTestRelay(t, 4)
	source := t.TempDir()
	receiveDir = t.TempDir()
	t.Chdir(receiveDir)
	write := func(dir string, entries map[string]string) []string {
		var paths []string
		for name, content := range entries {
			var err error
			if strings.HasSuffix(name, "/") {
				err = os.MkdirAll(filepath.Join(dir, name), 0o755)
			} else {
				err = os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755)
				if err == nil {
					err = os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			paths = append(paths, filepath.Join(dir, name))
		}
		return paths
	}
	write(receiveDir, existing)
	filesInfo, folders, totalFolders, err := GetFilesInfo(write(source, files), false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	secret := fmt.Sprintf("prompt-%d", time.Now().UnixNano())
	for _, o := range []*Options{&sender, &receiver} {
		o.SharedSecret, o.RelayAddress, o.RelayPassword = secret, relay.address, "pass123"
		o.DisableLocal, o.DisableClipboard, o.Curve = true, true, "siec"
	}
	sender.IsSender, sender.Transport, sender.RelayPorts = true, TransportRelay, slices.Clone(relay.allPorts)
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
	return receiveDir, errs[0], errs[1]
}

type questionLog struct {
	mu        sync.Mutex
	questions []Question
	answer    func(Question) bool
}

func (l *questionLog) prompt(_ context.Context, q Question) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.questions = append(l.questions, q)
	return l.answer(q), nil
}

func (l *questionLog) of(kind QuestionKind) (names []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, q := range l.questions {
		if q.Kind == kind {
			if kind == QuestionOverwrite && !q.Required {
				panic("overwrite question is not Required: croc asks it under NoPrompt")
			}
			names = append(names, q.Name)
		}
	}
	slices.Sort(names)
	return names
}

func TestPromptAcceptsUnderNoPrompt(t *testing.T) {
	// One file: a multi-file send trips the existing race on the sender's TotalSent reset.
	files := map[string]string{"a.txt": "hello"}
	log := questionLog{answer: func(Question) bool { return true }}
	dir, sendErr, receiveErr := promptTransfer(t, files, nil, Options{NoPrompt: true}, Options{NoPrompt: true, Prompt: log.prompt})
	if sendErr != nil || receiveErr != nil {
		t.Fatalf("send error %v, receive error %v", sendErr, receiveErr)
	}
	if len(log.questions) != 1 || log.questions[0].Kind != QuestionAccept || log.questions[0].Required || len(log.questions[0].Files) != 1 {
		t.Errorf("questions = %+v, want one non-required accept question for the file", log.questions)
	}
	for name, want := range files {
		if got, _ := os.ReadFile(filepath.Join(dir, name)); string(got) != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestPromptCanRefuse(t *testing.T) {
	log := questionLog{answer: func(Question) bool { return false }}
	dir, sendErr, receiveErr := promptTransfer(t, map[string]string{"secret.txt": "payload"}, nil, Options{NoPrompt: true}, Options{Prompt: log.prompt})
	if sendErr == nil || receiveErr == nil {
		t.Fatalf("send error %v, receive error %v; want both to fail", sendErr, receiveErr)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("refused transfer wrote %v", entries)
	}
}

func TestPromptSenderAskForcesReceiverQuestion(t *testing.T) {
	log := questionLog{answer: func(Question) bool { return true }}
	_, sendErr, receiveErr := promptTransfer(t, map[string]string{"a.txt": "hello"}, nil,
		Options{NoPrompt: true, Ask: true, Prompt: log.prompt},
		Options{NoPrompt: true, Prompt: log.prompt})
	if sendErr != nil || receiveErr != nil {
		t.Fatalf("send error %v, receive error %v", sendErr, receiveErr)
	}
	for _, q := range log.questions {
		if !q.Required || q.Name == "" {
			t.Errorf("question %+v: want Required and a machine ID", q)
		}
	}
	if len(log.questions) != 2 || len(log.of(QuestionAccept)) != 1 || len(log.of(QuestionSend)) != 1 {
		t.Errorf("questions = %+v, want one accept and one send", log.questions)
	}
}

// An unchanged file is skipped without a question; a changed one is asked about.
func TestPromptAnswersFileOverwrite(t *testing.T) {
	log := questionLog{answer: func(Question) bool { return true }}
	dir, sendErr, receiveErr := promptTransfer(t,
		map[string]string{"same.txt": "same", "changed.txt": "new!"},
		map[string]string{"same.txt": "same", "changed.txt": "old"},
		Options{NoPrompt: true}, Options{NoPrompt: true, Prompt: log.prompt})
	if sendErr != nil || receiveErr != nil {
		t.Fatalf("send error %v, receive error %v", sendErr, receiveErr)
	}
	if got := log.of(QuestionOverwrite); !slices.Equal(got, []string{"changed.txt"}) {
		t.Errorf("overwrite questions = %v, want only changed.txt", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "changed.txt")); string(got) != "new!" {
		t.Errorf("changed.txt = %q, want the sender's content", got)
	}
}

// Of two offered empty folders, only the one that has content locally is asked about.
func TestPromptAnswersEmptyFolderOverwrite(t *testing.T) {
	log := questionLog{answer: func(q Question) bool { return q.Kind == QuestionAccept }}
	_, sendErr, receiveErr := promptTransfer(t,
		map[string]string{"full/": "", "blank/": ""},
		map[string]string{"full/keep.txt": "keep", "blank/": ""},
		Options{NoPrompt: true}, Options{NoPrompt: true, Prompt: log.prompt})
	if sendErr != nil || receiveErr != nil {
		t.Fatalf("send error %v, receive error %v", sendErr, receiveErr)
	}
	if got := log.of(QuestionOverwrite); !slices.Equal(got, []string{"full"}) {
		t.Errorf("overwrite questions = %v, want only full", got)
	}
}
