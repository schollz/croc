package crypt

import (
	"bytes"
	"crypto/sha1"
	"io/ioutil"
	"os"
	"testing"
)

var plain = [][]byte{
	[]byte("Nö, ich trinke keinen Tee, ich bin Atheist. --- Helge Schneider"),
	[]byte("I wish these damn scientists would leave intelligence to the experts. --- Gen. Richard Stillwell (CIA)"),
	[]byte("I want to die peacefully in my sleep like my grandfather, not screaming in terror like his passengers. --- Charlie Hall"),
	[]byte("NOTE 3: Each bit has the value either ZERO or ONE. --- ECMA-035 spec"),
	[]byte("Writing about music is like dancing about architecture. --- Frank Zappa"),
	[]byte("If you want to go somewhere, goto is the best way to get there. --- K Thompson"),
}

func TestEncryptDecrypt(t *testing.T) {
	enc := bytes.NewBuffer(nil)
	dec := bytes.NewBuffer(nil)
	password := []byte("test password")
	c := &Crypter{
		HashFunc: sha1.New,
		HashSize: sha1.Size,
		Key:      NewPbkdf2Key(password, 32),
	}
	defer c.Key.Reset()

	for _, src := range plain {
		enc.Reset()
		dec.Reset()
		err := c.Encrypt(enc, bytes.NewReader(src))
		if err != nil {
			t.Fatal(err)
		}
		err = c.Decrypt(dec, enc)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Compare(dec.Bytes(), src) != 0 {
			t.Errorf("encrypt/decrypt error: want %q, got %q", string(src), string(dec.Bytes()))
		}
	}
}

func TestEncryptDecrypt1(t *testing.T) {
	f, err := os.Open("crypt_test.go")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	encBuf := bytes.NewBuffer(nil)
	password := []byte("test password")
	c := &Crypter{
		HashFunc: sha1.New,
		HashSize: sha1.Size,
		Key:      NewPbkdf2Key(password, 32),
	}
	defer c.Key.Reset()

	err = c.Encrypt(encBuf, f)
	if err != nil {
		t.Fatal(err)
	}

	decBuf := bytes.NewBuffer(nil)
	err = c.Decrypt(decBuf, encBuf)
	if err != nil {
		t.Fatal(err)
	}

	src, err := ioutil.ReadFile("crypt_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Compare(decBuf.Bytes(), src) != 0 {
		t.Errorf("encrypt/decrypt file error: crypt_test.go")
	}
}
