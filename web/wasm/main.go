//go:build js && wasm

package main

import (
	"crypto/cipher"
	"crypto/sha256"
	"fmt"
	"hash"
	"math"
	"sync"
	"syscall/js"

	"github.com/cespare/xxhash/v2"
	"github.com/schollz/croc/v11/src/codephrase"
	croccompress "github.com/schollz/croc/v11/src/compress"
	"github.com/schollz/croc/v11/src/crypt"
	"github.com/schollz/croc/v11/src/pakekey"
	"github.com/schollz/croc/v11/src/storecrypto"
	"github.com/schollz/pake/v3"
)

type bridge struct {
	mu         sync.Mutex
	nextHandle int
	pakes      map[int]*pake.Pake
	hashes     map[int]*xxhash.Digest
	sha256s    map[int]hash.Hash
	ciphers    map[int]cipher.AEAD
	funcs      []js.Func
}

func main() {
	b := &bridge{
		pakes:   make(map[int]*pake.Pake),
		hashes:  make(map[int]*xxhash.Digest),
		sha256s: make(map[int]hash.Hash),
		ciphers: make(map[int]cipher.AEAD),
	}
	api := js.Global().Get("Object").New()
	b.expose(api, "pakeInit", b.pakeInit)
	b.expose(api, "pakeInitWithIdentities", b.pakeInitWithIdentities)
	b.expose(api, "pakeUpdate", b.pakeUpdate)
	b.expose(api, "deriveKey", b.deriveKey)
	b.expose(api, "derivePeerKeys", b.derivePeerKeys)
	b.expose(api, "confirmPeerKey", b.confirmPeerKey)
	b.expose(api, "encrypt", b.encrypt)
	b.expose(api, "decrypt", b.decrypt)
	b.expose(api, "compress", b.compress)
	b.expose(api, "decompress", b.decompress)
	b.expose(api, "cipherInit", b.cipherInit)
	b.expose(api, "cipherRelease", b.cipherRelease)
	b.expose(api, "encodeChunk", b.encodeChunk)
	b.expose(api, "decodeChunk", b.decodeChunk)
	b.expose(api, "hashInit", b.hashInit)
	b.expose(api, "hashUpdate", b.hashUpdate)
	b.expose(api, "hashFinal", b.hashFinal)
	b.expose(api, "codeComponents", b.codeComponents)
	b.expose(api, "relayIndex", b.relayIndex)
	b.expose(api, "sha256Init", b.sha256Init)
	b.expose(api, "sha256Update", b.sha256Update)
	b.expose(api, "sha256Final", b.sha256Final)
	b.expose(api, "storeGenerateKey", b.storeGenerateKey)
	b.expose(api, "storeRedeemCapability", b.storeRedeemCapability)
	b.expose(api, "storeSealManifest", b.storeSealManifest)
	b.expose(api, "storeOpenManifest", b.storeOpenManifest)
	b.expose(api, "storeSealChunk", b.storeSealChunk)
	b.expose(api, "storeOpenChunk", b.storeOpenChunk)
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

func (b *bridge) pakeInitWithIdentities(args []js.Value) (any, error) {
	if len(args) != 5 {
		return nil, fmt.Errorf("pakeInitWithIdentities expects password, role, curve, purpose, and room")
	}
	password, err := bytesFromJS(args[0])
	if err != nil {
		return nil, err
	}
	instance, err := pakekey.Init(
		password,
		args[1].Int(),
		args[2].String(),
		args[3].String(),
		args[4].String(),
	)
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

func (b *bridge) derivePeerKeys(args []js.Value) (any, error) {
	if len(args) != 7 {
		return nil, fmt.Errorf("derivePeerKeys expects session key, salt, purpose, room, curve, initiator, and responder")
	}
	sharedKey, err := bytesFromJS(args[0])
	if err != nil {
		return nil, err
	}
	salt, err := bytesFromJS(args[1])
	if err != nil {
		return nil, err
	}
	initiator, err := bytesFromJS(args[5])
	if err != nil {
		return nil, err
	}
	responder, err := bytesFromJS(args[6])
	if err != nil {
		return nil, err
	}
	keys, err := pakekey.Derive(sharedKey, pakekey.Context{
		Purpose:   args[2].String(),
		Room:      args[3].String(),
		Curve:     args[4].String(),
		Initiator: initiator,
		Responder: responder,
		Salt:      salt,
	})
	if err != nil {
		return nil, err
	}
	result := js.Global().Get("Object").New()
	result.Set("key", bytesToJS(keys.EncryptionKey))
	result.Set("confirmationA", bytesToJS(keys.ConfirmationA))
	result.Set("confirmationB", bytesToJS(keys.ConfirmationB))
	return result, nil
}

func (b *bridge) confirmPeerKey(args []js.Value) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("confirmPeerKey expects expected and received tags")
	}
	expected, err := bytesFromJS(args[0])
	if err != nil {
		return nil, err
	}
	received, err := bytesFromJS(args[1])
	if err != nil {
		return nil, err
	}
	return pakekey.Confirm(expected, received), nil
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
	if len(args) != 2 {
		return nil, fmt.Errorf("decompress expects bytes and a byte limit")
	}
	if args[1].Type() != js.TypeNumber {
		return nil, fmt.Errorf("decompress byte limit must be a non-negative safe integer")
	}
	maxOutputSize := args[1].Float()
	const maxSafeInteger = 1<<53 - 1
	if math.IsNaN(maxOutputSize) || math.IsInf(maxOutputSize, 0) ||
		maxOutputSize < 0 || maxOutputSize > maxSafeInteger || math.Trunc(maxOutputSize) != maxOutputSize {
		return nil, fmt.Errorf("decompress byte limit must be a non-negative safe integer")
	}
	input, err := bytesFromJS(args[0])
	if err != nil {
		return nil, err
	}
	decompressed, err := croccompress.Decompress(input, int64(maxOutputSize))
	if err != nil {
		return nil, err
	}
	return bytesToJS(decompressed), nil
}

func (b *bridge) cipherInit(args []js.Value) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("cipherInit expects a key")
	}
	key, err := bytesFromJS(args[0])
	if err != nil {
		return nil, err
	}
	aead, err := crypt.NewAESGCM(key)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	handle := b.allocateHandle()
	b.ciphers[handle] = aead
	b.mu.Unlock()
	return handle, nil
}

func (b *bridge) cipherRelease(args []js.Value) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("cipherRelease expects a handle")
	}
	b.mu.Lock()
	delete(b.ciphers, args[0].Int())
	b.mu.Unlock()
	return nil, nil
}

func (b *bridge) chunkCipher(handle int) (cipher.AEAD, error) {
	b.mu.Lock()
	aead := b.ciphers[handle]
	b.mu.Unlock()
	if aead == nil {
		return nil, fmt.Errorf("unknown cipher handle")
	}
	return aead, nil
}

func (b *bridge) encodeChunk(args []js.Value) (any, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("encodeChunk expects handle, bytes, and compression flag")
	}
	aead, err := b.chunkCipher(args[0].Int())
	if err != nil {
		return nil, err
	}
	input, err := bytesFromJS(args[1])
	if err != nil {
		return nil, err
	}
	if args[2].Bool() {
		input = croccompress.Compress(input)
	}
	encoded, err := crypt.EncryptAEAD(input, aead)
	if err != nil {
		return nil, err
	}
	return bytesToJS(encoded), nil
}

func (b *bridge) decodeChunk(args []js.Value) (any, error) {
	if len(args) != 4 {
		return nil, fmt.Errorf("decodeChunk expects handle, bytes, compression flag, and byte limit")
	}
	aead, err := b.chunkCipher(args[0].Int())
	if err != nil {
		return nil, err
	}
	input, err := bytesFromJS(args[1])
	if err != nil {
		return nil, err
	}
	decoded, err := crypt.DecryptAEADInPlace(input, aead)
	if err != nil {
		return nil, err
	}
	if args[2].Bool() {
		decoded, err = croccompress.Decompress(decoded, int64(args[3].Int()))
		if err != nil {
			return nil, err
		}
	}
	return bytesToJS(decoded), nil
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

func (b *bridge) codeComponents(args []js.Value) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("codeComponents expects a croc code")
	}
	components, err := codephrase.Parse(args[0].String())
	if err != nil {
		return nil, err
	}
	result := js.Global().Get("Object").New()
	result.Set("room", components.RoomName)
	result.Set("passphrase", components.PAKEPassphrase)
	result.Set("format", string(components.Format))
	return result, nil
}

func (b *bridge) relayIndex(args []js.Value) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("relayIndex expects a croc code and relay count")
	}
	return codephrase.RelayIndex(args[0].String(), args[1].Int())
}

func (b *bridge) sha256Init(args []js.Value) (any, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("sha256Init expects no arguments")
	}
	b.mu.Lock()
	handle := b.allocateHandle()
	b.sha256s[handle] = sha256.New()
	b.mu.Unlock()
	return handle, nil
}

func (b *bridge) sha256Update(args []js.Value) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("sha256Update expects handle and bytes")
	}
	input, err := bytesFromJS(args[1])
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	digest, exists := b.sha256s[args[0].Int()]
	b.mu.Unlock()
	if !exists {
		return nil, fmt.Errorf("unknown SHA-256 handle")
	}
	_, err = digest.Write(input)
	return nil, err
}

func (b *bridge) sha256Final(args []js.Value) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("sha256Final expects handle")
	}
	handle := args[0].Int()
	b.mu.Lock()
	digest, exists := b.sha256s[handle]
	delete(b.sha256s, handle)
	b.mu.Unlock()
	if !exists {
		return nil, fmt.Errorf("unknown SHA-256 handle")
	}
	return bytesToJS(digest.Sum(nil)), nil
}

func (b *bridge) storeGenerateKey(args []js.Value) (any, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("storeGenerateKey expects no arguments")
	}
	key, err := storecrypto.GenerateKey()
	if err != nil {
		return nil, err
	}
	return bytesToJS(key), nil
}

func (b *bridge) storeRedeemCapability(args []js.Value) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("storeRedeemCapability expects a master key")
	}
	key, err := bytesFromJS(args[0])
	if err != nil {
		return nil, err
	}
	capability, err := storecrypto.RedeemCapability(key)
	if err != nil {
		return nil, err
	}
	return bytesToJS(capability), nil
}

func (b *bridge) storeSealManifest(args []js.Value) (any, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("storeSealManifest expects key, id, and JSON")
	}
	key, err := bytesFromJS(args[0])
	if err != nil {
		return nil, err
	}
	plaintext, err := bytesFromJS(args[2])
	if err != nil {
		return nil, err
	}
	ciphertext, err := storecrypto.SealManifestJSON(key, args[1].String(), plaintext)
	if err != nil {
		return nil, err
	}
	return bytesToJS(ciphertext), nil
}

func (b *bridge) storeOpenManifest(args []js.Value) (any, error) {
	if len(args) != 4 {
		return nil, fmt.Errorf("storeOpenManifest expects key, id, ciphertext, and byte limit")
	}
	key, err := bytesFromJS(args[0])
	if err != nil {
		return nil, err
	}
	ciphertext, err := bytesFromJS(args[2])
	if err != nil {
		return nil, err
	}
	plaintext, err := storecrypto.OpenManifestJSON(
		key,
		args[1].String(),
		ciphertext,
		int64(args[3].Float()),
	)
	if err != nil {
		return nil, err
	}
	return bytesToJS(plaintext), nil
}

func storeChunkRef(args []js.Value) storecrypto.ChunkRef {
	return storecrypto.ChunkRef{
		ObjectIndex: args[2].Int(),
		FileIndex:   args[3].Int(),
		FileChunk:   args[4].Int(),
		PlainSize:   args[5].Int(),
	}
}

func (b *bridge) storeSealChunk(args []js.Value) (any, error) {
	if len(args) != 7 {
		return nil, fmt.Errorf("storeSealChunk expects key, id, indexes, size, and plaintext")
	}
	key, err := bytesFromJS(args[0])
	if err != nil {
		return nil, err
	}
	plaintext, err := bytesFromJS(args[6])
	if err != nil {
		return nil, err
	}
	ciphertext, err := storecrypto.SealChunk(
		key,
		args[1].String(),
		storeChunkRef(args),
		plaintext,
	)
	if err != nil {
		return nil, err
	}
	return bytesToJS(ciphertext), nil
}

func (b *bridge) storeOpenChunk(args []js.Value) (any, error) {
	if len(args) != 7 {
		return nil, fmt.Errorf("storeOpenChunk expects key, id, indexes, size, and ciphertext")
	}
	key, err := bytesFromJS(args[0])
	if err != nil {
		return nil, err
	}
	ciphertext, err := bytesFromJS(args[6])
	if err != nil {
		return nil, err
	}
	plaintext, err := storecrypto.OpenChunk(
		key,
		args[1].String(),
		storeChunkRef(args),
		ciphertext,
	)
	if err != nil {
		return nil, err
	}
	return bytesToJS(plaintext), nil
}
