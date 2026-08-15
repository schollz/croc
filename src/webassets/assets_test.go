package webassets

import (
	"bytes"
	"errors"
	"io/fs"
	"testing"
)

func TestEmbeddedClientContainsEntryPointAndWasm(t *testing.T) {
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
	wasm, err := fs.Stat(files, "croc.wasm")
	if err != nil {
		t.Fatalf("stat embedded WASM: %v", err)
	}
	if wasm.Size() == 0 {
		t.Fatal("embedded WASM is empty")
	}
	installer, err := fs.ReadFile(files, "default.txt")
	if err != nil {
		t.Fatalf("read embedded installer: %v", err)
	}
	for _, fragment := range [][]byte{
		[]byte("#!/bin/bash"),
		[]byte("curl https://getcroc.com | bash"),
	} {
		if !bytes.Contains(installer, fragment) {
			t.Fatalf("embedded installer does not contain %q", fragment)
		}
	}
}
