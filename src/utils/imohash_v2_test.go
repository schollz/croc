package utils

import (
	"bytes"
	"context"
	"encoding/hex"
	"io"
	"os"
	"testing"
)

type countingReaderAt struct {
	data []byte
	read int64
}

func (r *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := bytes.NewReader(r.data).ReadAt(p, off)
	r.read += int64(n)
	return n, err
}

func hashV2Bytes(t *testing.T, data []byte) []byte {
	t.Helper()
	hash, err := IMOHashV2Reader(io.NewSectionReader(bytes.NewReader(data), 0, int64(len(data))), nil)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func TestIMOHashV2Offsets(t *testing.T) {
	if got := IMOHashV2Offsets(1024); len(got) != 1 || got[0] != 0 {
		t.Fatalf("small offsets = %v", got)
	}
	size := int64(16 * 1024 * 1024)
	offsets := IMOHashV2Offsets(size)
	if len(offsets) != IMOHashV2WindowCount {
		t.Fatalf("got %d offsets", len(offsets))
	}
	if offsets[0] != 0 || offsets[len(offsets)-1] != size-IMOHashV2WindowSize {
		t.Fatalf("endpoints = %d, %d", offsets[0], offsets[len(offsets)-1])
	}
	for i := 1; i < len(offsets); i++ {
		if offsets[i] <= offsets[i-1] {
			t.Fatalf("offsets not ordered: %v", offsets)
		}
	}
}

func TestIMOHashV2Vectors(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{name: "empty", data: nil, want: "75b1d12f38872e46826e21c2bc1ddacd"},
		{name: "hello", data: []byte("hello"), want: "685a85c3c0c2484da70684b1cccc22d7"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := hex.EncodeToString(hashV2Bytes(t, test.data))
			if got != test.want {
				t.Fatalf("got %s, want %s", got, test.want)
			}
		})
	}
}

func TestIMOHashV2ReadBudgetAndMutations(t *testing.T) {
	size := int(IMOHashV2SmallFileLimit + 4*1024*1024)
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i*31 + 7)
	}
	reader := &countingReaderAt{data: data}
	original, err := IMOHashV2Reader(io.NewSectionReader(reader, 0, int64(size)), nil)
	if err != nil {
		t.Fatal(err)
	}
	if reader.read > IMOHashV2WindowSize*IMOHashV2WindowCount {
		t.Fatalf("read %d bytes, budget %d", reader.read, IMOHashV2WindowSize*IMOHashV2WindowCount)
	}

	sampled := append([]byte(nil), data...)
	sampled[IMOHashV2Offsets(int64(size))[4]+123] ^= 0xff
	if bytes.Equal(original, hashV2Bytes(t, sampled)) {
		t.Fatal("sampled mutation was not detected")
	}

	unsampled := append([]byte(nil), data...)
	offsets := IMOHashV2Offsets(int64(size))
	gap := offsets[0] + IMOHashV2WindowSize + (offsets[1]-offsets[0]-IMOHashV2WindowSize)/2
	unsampled[gap] ^= 0xff
	if !bytes.Equal(original, hashV2Bytes(t, unsampled)) {
		t.Fatal("unsampled mutation unexpectedly changed progressive digest")
	}
}

func TestIMOHashV2SmallFilesAreReadCompletely(t *testing.T) {
	data := bytes.Repeat([]byte("abcdefgh"), 1024)
	original := hashV2Bytes(t, data)
	for _, index := range []int{0, len(data) / 2, len(data) - 1} {
		changed := append([]byte(nil), data...)
		changed[index] ^= 1
		if bytes.Equal(original, hashV2Bytes(t, changed)) {
			t.Fatalf("mutation at %d was not detected", index)
		}
	}
}

func TestIMOHashV2Cancellation(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "imohash-v2-*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(bytes.Repeat([]byte{1}, 1024)); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = HashFileCtx(ctx, file.Name(), "imohash-v2"); err == nil {
		t.Fatal("canceled hash succeeded")
	}
}
