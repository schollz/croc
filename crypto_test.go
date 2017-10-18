package main

import (
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
