package main

import (
	"io/ioutil"
	"os"
	"testing"
)

func TestEncrypt(t *testing.T) {
	key := GetRandomName()
	encrypted, salt, iv := Encrypt([]byte("hello, world"), key)
	decrypted, err := Decrypt(encrypted, key, salt, iv)
	if err != nil {
		t.Error(err)
	}
	if string(decrypted) != "hello, world" {
		t.Error("problem decrypting")
	}
	_, err = Decrypt(encrypted, "wrong passphrase", salt, iv)
	if err == nil {
		t.Error("should not work!")
	}
}

func TestEncryptFiles(t *testing.T) {
	key := GetRandomName()
	if err := ioutil.WriteFile("temp", []byte("hello, world!"), 0644); err != nil {
		t.Error(err)
	}
	if err := EncryptFile("temp", "temp.enc", key); err != nil {
		t.Error(err)
	}
	if err := DecryptFile("temp.enc", "temp.dec", key); err != nil {
		t.Error(err)
	}
	data, err := ioutil.ReadFile("temp.dec")
	if string(data) != "hello, world!" {
		t.Errorf("Got something weird: " + string(data))
	}
	if err != nil {
		t.Error(err)
	}
	if err := DecryptFile("temp.enc", "temp.dec", key+"wrong password"); err == nil {
		t.Error("should throw error!")
	}
	os.Remove("temp.dec")
	os.Remove("temp.enc")

}
