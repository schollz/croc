//go:build js && wasm

package main

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strconv"
	"sync"
	"syscall/js"

	"github.com/cespare/xxhash/v2"
	croccompress "github.com/schollz/croc/v10/src/compress"
	"github.com/schollz/croc/v10/src/crypt"
	"github.com/schollz/croc/v10/src/mnemonicode"
	"github.com/schollz/pake/v3"
)

type bridge struct {
	mu         sync.Mutex
	nextHandle int
	pakes      map[int]*pake.Pake
	hashes     map[int]*xxhash.Digest
	funcs      []js.Func
}

func main() {
	b := &bridge{
		pakes:  make(map[int]*pake.Pake),
		hashes: make(map[int]*xxhash.Digest),
	}
	api := js.Global().Get("Object").New()
	b.expose(api, "pakeInit", b.pakeInit)
	b.expose(api, "pakeUpdate", b.pakeUpdate)
	b.expose(api, "deriveKey", b.deriveKey)
	b.expose(api, "encrypt", b.encrypt)
	b.expose(api, "decrypt", b.decrypt)
	b.expose(api, "compress", b.compress)
	b.expose(api, "decompress", b.decompress)
	b.expose(api, "hashInit", b.hashInit)
	b.expose(api, "hashUpdate", b.hashUpdate)
	b.expose(api, "hashFinal", b.hashFinal)
	b.expose(api, "randomCode", b.randomCode)
	js.Global().Set("crocWasm", api)
	select {}
}

func (b *bridge) expose(api js.Value, name string, fn func([]js.Value) (any, error)) {
	wrapped := js.FuncOf(func(_ js.Value, args []js.Value) any {
		result, err := safeCall(fn, args)
		response := js.Global().Get("Object").New()
		if err != nil {
			response.Set("ok", false)
			response.Set("error", err.Error())
			return response
		}
		response.Set("ok", true)
		if result != nil {
			response.Set("value", result)
		}
		return response
	})
	b.funcs = append(b.funcs, wrapped)
	api.Set(name, wrapped)
}

func safeCall(fn func([]js.Value) (any, error), args []js.Value) (result any, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("wasm bridge panic: %v", recovered)
		}
	}()
	return fn(args)
}

func bytesFromJS(value js.Value) ([]byte, error) {
	if value.Type() != js.TypeObject {
		return nil, fmt.Errorf("expected Uint8Array")
	}
	bytes := make([]byte, value.Get("byteLength").Int())
	if copied := js.CopyBytesToGo(bytes, value); copied != len(bytes) {
		return nil, fmt.Errorf("copied %d of %d bytes", copied, len(bytes))
	}
	return bytes, nil
}

func bytesToJS(bytes []byte) js.Value {
	value := js.Global().Get("Uint8Array").New(len(bytes))
	js.CopyBytesToJS(value, bytes)
	return value
}

func (b *bridge) allocateHandle() int {
	b.nextHandle++
	return b.nextHandle
}

func (b *bridge) pakeInit(args []js.Value) (any, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("pakeInit expects password, role, and curve")
	}
	password, err := bytesFromJS(args[0])
	if err != nil {
		return nil, err
	}
	instance, err := pake.InitCurve(password, args[1].Int(), args[2].String())
	if err != nil {
		return nil, err
	}

	b.mu.Lock()
	handle := b.allocateHandle()
	b.pakes[handle] = instance
	b.mu.Unlock()

	result := js.Global().Get("Object").New()
	result.Set("handle", handle)
	result.Set("bytes", bytesToJS(instance.Bytes()))
	return result, nil
}

func (b *bridge) pakeUpdate(args []js.Value) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("pakeUpdate expects handle and peer bytes")
	}
	handle := args[0].Int()
	peerBytes, err := bytesFromJS(args[1])
	if err != nil {
		return nil, err
	}

	b.mu.Lock()
	instance, exists := b.pakes[handle]
	delete(b.pakes, handle)
	b.mu.Unlock()
	if !exists {
		return nil, fmt.Errorf("unknown PAKE handle")
	}
	if err = instance.Update(peerBytes); err != nil {
		return nil, err
	}
	key, err := instance.SessionKey()
	if err != nil {
		return nil, err
	}
	result := js.Global().Get("Object").New()
	result.Set("bytes", bytesToJS(instance.Bytes()))
	result.Set("key", bytesToJS(key))
	return result, nil
}

func (b *bridge) deriveKey(args []js.Value) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("deriveKey expects passphrase and salt")
	}
	passphrase, err := bytesFromJS(args[0])
	if err != nil {
		return nil, err
	}
	salt, err := bytesFromJS(args[1])
	if err != nil {
		return nil, err
	}
	key, _, err := crypt.New(passphrase, salt)
	if err != nil {
		return nil, err
	}
	return bytesToJS(key), nil
}

func (b *bridge) encrypt(args []js.Value) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("encrypt expects plaintext and key")
	}
	plaintext, err := bytesFromJS(args[0])
	if err != nil {
		return nil, err
	}
	key, err := bytesFromJS(args[1])
	if err != nil {
		return nil, err
	}
	encrypted, err := crypt.Encrypt(plaintext, key)
	if err != nil {
		return nil, err
	}
	return bytesToJS(encrypted), nil
}

func (b *bridge) decrypt(args []js.Value) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("decrypt expects ciphertext and key")
	}
	ciphertext, err := bytesFromJS(args[0])
	if err != nil {
		return nil, err
	}
	key, err := bytesFromJS(args[1])
	if err != nil {
		return nil, err
	}
	plaintext, err := crypt.Decrypt(ciphertext, key)
	if err != nil {
		return nil, err
	}
	return bytesToJS(plaintext), nil
}

func (b *bridge) compress(args []js.Value) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("compress expects bytes")
	}
	input, err := bytesFromJS(args[0])
	if err != nil {
		return nil, err
	}
	return bytesToJS(croccompress.Compress(input)), nil
}

func (b *bridge) decompress(args []js.Value) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("decompress expects bytes")
	}
	input, err := bytesFromJS(args[0])
	if err != nil {
		return nil, err
	}
	return bytesToJS(croccompress.Decompress(input)), nil
}

func (b *bridge) hashInit(args []js.Value) (any, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("hashInit expects no arguments")
	}
	b.mu.Lock()
	handle := b.allocateHandle()
	b.hashes[handle] = xxhash.New()
	b.mu.Unlock()
	return handle, nil
}

func (b *bridge) hashUpdate(args []js.Value) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("hashUpdate expects handle and bytes")
	}
	input, err := bytesFromJS(args[1])
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	hash, exists := b.hashes[args[0].Int()]
	b.mu.Unlock()
	if !exists {
		return nil, fmt.Errorf("unknown hash handle")
	}
	_, err = hash.Write(input)
	return nil, err
}

func (b *bridge) hashFinal(args []js.Value) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("hashFinal expects handle")
	}
	handle := args[0].Int()
	b.mu.Lock()
	hash, exists := b.hashes[handle]
	delete(b.hashes, handle)
	b.mu.Unlock()
	if !exists {
		return nil, fmt.Errorf("unknown hash handle")
	}
	return bytesToJS(hash.Sum(nil)), nil
}

func (b *bridge) randomCode(args []js.Value) (any, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("randomCode expects no arguments")
	}
	pin := ""
	max := big.NewInt(9)
	for range 4 {
		digit, err := rand.Int(rand.Reader, max)
		if err != nil {
			return nil, err
		}
		pin += strconv.FormatInt(digit.Int64(), 10)
	}
	entropy := make([]byte, 4)
	if _, err := rand.Read(entropy); err != nil {
		return nil, err
	}
	return pin + "-" + joinWords(mnemonicode.EncodeWordList(nil, entropy)), nil
}

func joinWords(words []string) string {
	result := ""
	for index, word := range words {
		if index > 0 {
			result += "-"
		}
		result += word
	}
	return result
}
