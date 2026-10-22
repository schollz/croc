// Test-only JSON-lines oracle. It deliberately imports the CLI's pinned packages.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/cespare/xxhash/v2"
	"github.com/schollz/croc/v11/src/codephrase"
	"github.com/schollz/croc/v11/src/compress"
	"github.com/schollz/croc/v11/src/crypt"
	"github.com/schollz/croc/v11/src/pakekey"
	"github.com/schollz/pake/v3"
	"os"
)

type request struct {
	Method                                           string
	Password, Bytes, Key, Salt, Initiator, Responder []byte
	Role, Handle, Count                              int
	Curve, Purpose, Room, Code                       string
	Limit                                            int64
}

var instances = map[int]*pake.Pake{}
var next int

func call(q request) (any, error) {
	switch q.Method {
	case "init":
		var p *pake.Pake
		var err error
		if q.Purpose == "" {
			p, err = pake.InitCurve(q.Password, q.Role, q.Curve)
		} else {
			p, err = pakekey.Init(q.Password, q.Role, q.Curve, q.Purpose, q.Room)
		}
		if err != nil {
			return nil, err
		}
		next++
		instances[next] = p
		return map[string]any{"handle": next, "bytes": p.Bytes()}, nil
	case "wire":
		var p pake.Pake
		if err := json.Unmarshal(q.Bytes, &p); err != nil {
			return nil, err
		}
		return p.Bytes(), nil
	case "seal":
		aead, err := crypt.NewAESGCM(q.Key)
		if err != nil {
			return nil, err
		}
		return aead.Seal(append([]byte{}, q.Salt...), q.Salt, q.Bytes, nil), nil
	case "update":
		p := instances[q.Handle]
		delete(instances, q.Handle)
		if p == nil {
			return nil, fmt.Errorf("unknown handle")
		}
		if err := p.Update(q.Bytes); err != nil {
			return nil, err
		}
		key, err := p.SessionKey()
		return map[string]any{"bytes": p.Bytes(), "key": key}, err
	case "derive":
		k, _, err := crypt.New(q.Password, q.Salt)
		return k, err
	case "peer":
		k, err := pakekey.Derive(q.Key, pakekey.Context{Purpose: q.Purpose, Room: q.Room, Curve: q.Curve, Initiator: q.Initiator, Responder: q.Responder, Salt: q.Salt})
		return map[string]any{"key": k.EncryptionKey, "confirmationA": k.ConfirmationA, "confirmationB": k.ConfirmationB}, err
	case "encrypt":
		return crypt.Encrypt(q.Bytes, q.Key)
	case "decrypt":
		return crypt.Decrypt(q.Bytes, q.Key)
	case "compress":
		return compress.Compress(q.Bytes), nil
	case "decompress":
		return compress.Decompress(q.Bytes, q.Limit)
	case "hashPattern":
		q.Bytes = make([]byte, q.Count)
		for i := range q.Bytes {
			q.Bytes[i] = byte((i*37 + i%31) % 256)
		}
		fallthrough
	case "hash":
		h := xxhash.New()
		h.Write(q.Bytes)
		return h.Sum(nil), nil
	case "code":
		c, err := codephrase.Parse(q.Code)
		return map[string]any{"room": c.RoomName, "passphrase": c.PAKEPassphrase, "format": c.Format}, err
	case "sshCode":
		c, err := codephrase.ParseSSH(q.Code)
		return map[string]any{"room": c.RoomName, "passphrase": c.PAKEPassphrase}, err
	case "relay":
		return codephrase.RelayIndex(q.Code, q.Count)
	}
	return nil, fmt.Errorf("unknown method")
}
func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 8<<20)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var q request
		err := json.Unmarshal(scanner.Bytes(), &q)
		var result any
		if err == nil {
			result, err = call(q)
		}
		if err != nil {
			encoder.Encode(map[string]any{"error": err.Error()})
		} else {
			encoder.Encode(map[string]any{"result": result})
		}
	}
}
