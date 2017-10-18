package main

import (
	"fmt"
	"testing"
)

func TestEncrypt(t *testing.T) {
	key := GetRandomName()
	fmt.Println(key)
	salt, iv, encrypted := Encrypt([]byte("hello, world"), key)
	fmt.Println(len(encrypted))
	decrypted, err := Decrypt(salt, iv, encrypted, key)
	if err != nil {
		t.Error(err)
	}
	if string(decrypted) != "hello, world" {
		t.Error("problem decrypting")
	}
	_, err = Decrypt(salt, iv, encrypted, "wrong passphrase")
	if err == nil {
		t.Error("should not work!")
	}
}
