package croc

import (
	"io/ioutil"
	"os"
	"testing"
)

func TestEncrypt(t *testing.T) {
	key := getRandomName()
	encrypted, salt, iv := encrypt([]byte("hello, world"), key)
	decrypted, err := decrypt(encrypted, key, salt, iv)
	if err != nil {
		t.Error(err)
	}
	if string(decrypted) != "hello, world" {
		t.Error("problem decrypting")
	}
	_, err = decrypt(encrypted, "wrong passphrase", salt, iv)
	if err == nil {
		t.Error("should not work!")
	}
}

func TestEncryptFiles(t *testing.T) {
	key := getRandomName()
	if err := ioutil.WriteFile("temp", []byte("hello, world!"), 0644); err != nil {
		t.Error(err)
	}
	if err := encryptFile("temp", "temp.enc", key); err != nil {
		t.Error(err)
	}
	if err := decryptFile("temp.enc", "temp.dec", key); err != nil {
		t.Error(err)
	}
	data, err := ioutil.ReadFile("temp.dec")
	if string(data) != "hello, world!" {
		t.Errorf("Got something weird: " + string(data))
	}
	if err != nil {
		t.Error(err)
	}
	if err := decryptFile("temp.enc", "temp.dec", key+"wrong password"); err == nil {
		t.Error("should throw error!")
	}
	os.Remove("temp.dec")
	os.Remove("temp.enc")
	os.Remove("temp")
}
