package webassets

import (
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
	wasm, err := fs.Stat(files, "croc.wasm")
	if err != nil {
		t.Fatalf("stat embedded WASM: %v", err)
	}
	if wasm.Size() == 0 {
		t.Fatal("embedded WASM is empty")
	}
}
