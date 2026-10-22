package webassets

import (
	"bytes"
	"errors"
	"io/fs"
	"regexp"
	"testing"
)

func TestEmbeddedClientContainsNativeWorkersAndSSHWasm(t *testing.T) {
	files := Files()
	index, err := fs.ReadFile(files, "index.html")
	if errors.Is(err, fs.ErrNotExist) {
		t.Skip("generated web client is absent; run npm --prefix web run embed")
	}
	if err != nil {
		t.Fatalf("read embedded index: %v", err)
	}
	if len(index) == 0 {
		t.Fatal("embedded index is empty")
	}
	for _, fragment := range [][]byte{
		[]byte(`rel="canonical" href="https://getcroc.com/"`),
		[]byte(`property="og:title"`),
		[]byte(`name="twitter:card"`),
		[]byte(`type="application/ld+json"`),
		[]byte(`"@type": "WebApplication"`),
		[]byte(`"@type": "AggregateRating"`),
		[]byte(`"ratingCount": 50`),
		[]byte(`"reviewCount": 50`),
		[]byte(`"reviewBody": "I use croc here a lot. Awesome binary for me"`),
	} {
		if !bytes.Contains(index, fragment) {
			t.Fatalf("embedded index does not contain metadata %q", fragment)
		}
	}
	article, err := fs.ReadFile(files, "blog/pake-step-by-step/index.html")
	if err != nil {
		t.Fatalf("read embedded PAKE article: %v", err)
	}
	for _, fragment := range [][]byte{
		[]byte(`<title>PAKE, step by step | croc field notes</title>`),
		[]byte(`rel="canonical" href="https://getcroc.com/blog/pake-step-by-step"`),
		[]byte(`property="og:image" content="https://getcroc.com/blog/images/pake-step-by-step.jpg"`),
		[]byte(`name="twitter:card" content="summary_large_image"`),
		[]byte(`"@type":"BlogPosting"`),
	} {
		if !bytes.Contains(article, fragment) {
			t.Fatalf("embedded PAKE article does not contain metadata %q", fragment)
		}
	}
	shareImage, err := fs.Stat(files, "blog/images/pake-step-by-step.jpg")
	if err != nil {
		t.Fatalf("stat embedded PAKE share image: %v", err)
	}
	if shareImage.Size() < 50_000 {
		t.Fatalf("embedded PAKE share image is unexpectedly small: %d", shareImage.Size())
	}
	v114Update, err := fs.ReadFile(files, "blog/croc-v11-4-release-update/index.html")
	if err != nil {
		t.Fatalf("read embedded v11.4 update: %v", err)
	}
	for _, fragment := range [][]byte{
		[]byte(`<title>croc v11.4: Secure Terminal Sharing from CLI and Browser</title>`),
		[]byte(`rel="canonical" href="https://getcroc.com/blog/croc-v11-4-release-update"`),
		[]byte(`property="og:image" content="https://getcroc.com/blog/images/croc-v11-4-release-update.jpg"`),
		[]byte(`"articleSection":"Updates"`),
		[]byte(`https://getcroc.com/blog/share-terminal-with-croc-ssh`),
	} {
		if !bytes.Contains(v114Update, fragment) {
			t.Fatalf("embedded v11.4 update does not contain metadata %q", fragment)
		}
	}
	v114Image, err := fs.Stat(files, "blog/images/croc-v11-4-release-update.jpg")
	if err != nil {
		t.Fatalf("stat embedded v11.4 share image: %v", err)
	}
	if v114Image.Size() < 50_000 {
		t.Fatalf("embedded v11.4 share image is unexpectedly small: %d", v114Image.Size())
	}
	for _, name := range []string{"croc.wasm", "croc-worker.js"} {
		if _, err := fs.Stat(files, name); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("obsolete direct-transfer asset %s is embedded: %v", name, err)
		}
	}
	protocolWorkers, err := fs.Glob(files, "assets/engine-worker-*.js")
	if err != nil || len(protocolWorkers) != 1 {
		t.Fatalf("expected one native protocol worker: %v, %v", protocolWorkers, err)
	}
	for _, name := range []string{"croc-ssh-worker.js", "wasm_exec.js"} {
		if info, err := fs.Stat(files, name); err != nil || info.Size() == 0 {
			t.Fatalf("missing retained SSH runtime asset %s: %v", name, err)
		}
	}
	hashWorkers, err := fs.Glob(files, "assets/sha256-worker-*.js")
	if err != nil || len(hashWorkers) != 1 {
		t.Fatalf("expected one embedded native SHA-256 worker: %v, %v", hashWorkers, err)
	}
	if bytes.Contains(index, []byte(`href="/croc.wasm"`)) {
		t.Fatal("entry point eagerly preloads the live-transfer WASM runtime")
	}
	sshWasm, err := fs.Stat(files, "croc-ssh.wasm")
	if err != nil {
		t.Fatalf("stat embedded SSH WASM: %v", err)
	}
	if sshWasm.Size() == 0 {
		t.Fatal("embedded SSH WASM is empty")
	}
	installer, err := fs.ReadFile(files, "default.txt")
	if err != nil {
		t.Fatalf("read embedded installer: %v", err)
	}
	for _, fragment := range [][]byte{
		[]byte("#!/bin/bash"),
		[]byte("curl https://getcroc.com | bash"),
		[]byte("https://api.github.com/repos/schollz/croc/releases/latest"),
		[]byte("determine_latest_version"),
	} {
		if !bytes.Contains(installer, fragment) {
			t.Fatalf("embedded installer does not contain %q", fragment)
		}
	}
	if regexp.MustCompile(`croc_version="[0-9]+\.[0-9]+\.[0-9]+"`).Match(installer) {
		t.Fatal("embedded installer contains a hardcoded croc version")
	}
}
