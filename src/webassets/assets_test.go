package webassets

import (
	"bytes"
	"io/fs"
	"testing"
)

func TestEmbeddedClientContainsEntryPointAndWasm(t *testing.T) {
	files := Files()
	index, err := fs.ReadFile(files, "index.html")
	if err != nil {
		t.Fatalf("read embedded index: %v", err)
	}
	if len(index) == 0 {
		t.Fatal("embedded index is empty")
	}
	for _, fragment := range [][]byte{
		[]byte(`rel="canonical" href="https://share.schollz.com/"`),
		[]byte(`property="og:title"`),
		[]byte(`name="twitter:card"`),
		[]byte(`type="application/ld+json"`),
		[]byte(`"@type": "WebApplication"`),
	} {
		if !bytes.Contains(index, fragment) {
			t.Fatalf("embedded index does not contain metadata %q", fragment)
		}
	}
	wasm, err := fs.Stat(files, "croc.wasm")
	if err != nil {
		t.Fatalf("stat embedded WASM: %v", err)
	}
	if wasm.Size() == 0 {
		t.Fatal("embedded WASM is empty")
	}
}
