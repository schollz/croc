package main

import (
	"fmt"
	"testing"
)

func TestEncrypt(t *testing.T) {
	key := GetRandomName()
	fmt.Println(key)
	encrypted, err := Encrypt([]byte("hello, world"), key)
	if err != nil {
		t.Error(err)
	}
	fmt.Println(len(encrypted))
	decrypted, err := Decrypt(encrypted, key)
	if err != nil {
		t.Error(err)
	}
	if string(decrypted) != "hello, world" {
		t.Error("problem decrypting")
	}
	_, err = Decrypt(encrypted, "wrong passphrase")
	if err == nil {
		t.Error("should not work!")
	}
}
