package main

//go:generate cp /usr/local/go/misc/wasm/wasm_exec.js .

// compile with
// GOOS=js GOARCH=wasm go build -o main.wasm

// to run
//
// bob = pakeInit("pass1","0");
// jane = pakeInit("pass1","1");
// jane = pakeUpdate(jane,pakePublic(bob));
// bob = pakeUpdate(bob,pakePublic(jane));
// jane = pakeUpdate(jane,pakePublic(bob));
// keyAndSalt = JSON.parse(pakeSessionKey(bob,""))
// console.log(pakeSessionKey(jane,keyAndSalt.Salt))

import (
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"syscall/js"
	"time"

	"github.com/pkg/errors"
	"github.com/schollz/croc/v7/src/box"
	"github.com/schollz/croc/v7/src/crypt"
	"github.com/schollz/croc/v7/src/models"
	log "github.com/schollz/logger"
	"github.com/schollz/pake/v2"
)

// writeWebsocketMessage(message,payload,key)
// if key == "", then no encryption is used
func writeWebsocketMessage(this js.Value, inputs []js.Value) interface{} {
	// initialize sender P ("0" indicates sender)
	if len(inputs) != 3 {
		return js.Global().Get("Error").New("need message, payload, key")
	}
	var key []byte
	key = nil
	var err error
	if len(inputs[2].String()) > 0 {
		key, err = base64.StdEncoding.DecodeString(inputs[2].String())
		if err != nil {
			return js.Global().Get("Error").New(err.Error())
		}
	}

	wsmsg := models.WebsocketMessage{
		Message: inputs[0].String(),
		Payload: inputs[1].String(),
	}
	bundled, err := box.Bundle(wsmsg, key)
	if err != nil {
		return js.Global().Get("Error").New(err.Error())
	}
	return bundled
}

// readWebsocketMessage(bundled,key)
// returns stringified JSON
// if key == "", then no decryption is used
func readWebsocketMessage(this js.Value, inputs []js.Value) interface{} {
	// initialize sender P ("0" indicates sender)
	if len(inputs) != 2 {
		return js.Global().Get("Error").New("need bundled, key")
	}
	var key []byte
	key = nil
	var err error
	if len(inputs[1].String()) > 0 {
		key, err = base64.StdEncoding.DecodeString(inputs[1].String())
		if err != nil {
			return js.Global().Get("Error").New(err.Error())
		}
	}

	var wsmsg models.WebsocketMessage

	err = box.Unbundle(inputs[0].String(), key, &wsmsg)
	if err != nil {
		return js.Global().Get("Error").New(err.Error())
	}

	b, err := json.Marshal(wsmsg)
	if err != nil {
		return js.Global().Get("Error").New(err.Error())
	}
	return string(b)
}

// pakeInit(weakPassphrase, role)
// returns: pakeBytes
func pakeInit(this js.Value, inputs []js.Value) interface{} {
	// initialize sender P ("0" indicates sender)
	if len(inputs) != 2 {
		return js.Global().Get("Error").New("need weakPassphrase, role")
	}
	role := 0
	if inputs[1].String() == "1" {
		role = 1
	}
	P, err := pake.Init([]byte(inputs[0].String()), role, elliptic.P521(), 1*time.Microsecond)
	if err != nil {
		log.Error(err)
		return js.Global().Get("Error").New(err.Error())
	}
	bJSON, err := json.Marshal(P)
	if err != nil {
		log.Error(err)
		return js.Global().Get("Error").New(err.Error())
	}
	return base64.StdEncoding.EncodeToString(bJSON)
}

// pakeUpdate(pakeBytes,otherPublicPakeBytes)
func pakeUpdate(this js.Value, inputs []js.Value) interface{} {
	if len(inputs) != 2 {
		return js.Global().Get("Error").New("need two input")
	}
	var P *pake.Pake

	b, err := base64.StdEncoding.DecodeString(inputs[0].String())
	if err != nil {
		log.Errorf("problem with %s: %s", inputs[0].String(), err)
		return js.Global().Get("Error").New(err.Error())
	}
	err = json.Unmarshal(b, &P)
	P.SetCurve(elliptic.P521())
	if err != nil {
		log.Error(err)
		return js.Global().Get("Error").New(err.Error())
	}

	qbytes, err := base64.StdEncoding.DecodeString(inputs[1].String())
	if err != nil {
		log.Errorf("problem with %s: %s", inputs[1].String(), err)
		return js.Global().Get("Error").New(err.Error())
	}
	err = P.Update(qbytes)
	if err != nil {
		log.Error(err)
		return js.Global().Get("Error").New(err.Error())
	}
	bJSON, err := json.Marshal(P)
	if err != nil {
		log.Error(err)
		return js.Global().Get("Error").New(err.Error())
	}
	return base64.StdEncoding.EncodeToString(bJSON)
}

// pakePublic(pakeBytes)
func pakePublic(this js.Value, inputs []js.Value) interface{} {
	var P *pake.Pake
	b, err := base64.StdEncoding.DecodeString(inputs[0].String())
	if err != nil {
		log.Error(err)
		return js.Global().Get("Error").New(err.Error())
	}
	err = json.Unmarshal(b, &P)
	if err != nil {
		log.Error(err)
		return js.Global().Get("Error").New(err.Error())
	}
	P.SetCurve(elliptic.P521())
	return base64.StdEncoding.EncodeToString(P.Bytes())
}

// pakeSessionKey(pakeBytes,salt)
func pakeSessionKey(this js.Value, inputs []js.Value) interface{} {
	if len(inputs) != 2 {
		return js.Global().Get("Error").New("need two input")
	}
	var P *pake.Pake
	b, err := base64.StdEncoding.DecodeString(inputs[0].String())
	if err != nil {
		err = errors.Wrap(err, "could not decode pakeBytes")
		return js.Global().Get("Error").New(err.Error())
	}
	err = json.Unmarshal(b, &P)
	P.SetCurve(elliptic.P521())
	if err != nil {
		return js.Global().Get("Error").New(err.Error())
	}
	key, err := P.SessionKey()
	if err != nil {
		return js.Global().Get("Error").New(err.Error())
	}

	type KeyAndSalt struct {
		Key  string
		Salt string
	}

	var kas KeyAndSalt
	var salt []byte
	salt = nil
	if len(inputs[1].String()) > 0 {
		b, errb := base64.StdEncoding.DecodeString(inputs[1].String())
		if errb != nil {
			return js.Global().Get("Error").New(errb.Error())
		}
		salt = b
	}

	cryptKey, cryptSalt, err := crypt.New(key, salt)

	kas.Key = base64.StdEncoding.EncodeToString(cryptKey)
	kas.Salt = base64.StdEncoding.EncodeToString(cryptSalt)
	b, _ = json.Marshal(kas)

	log.Debugf("key: %x", cryptKey)
	log.Debugf("salt: %x", cryptSalt)
	return string(b)
}

func main() {
	c := make(chan bool)
	// fmt.Println("starting")
	js.Global().Set("pakeInit", js.FuncOf(pakeInit))
	js.Global().Set("pakePublic", js.FuncOf(pakePublic))
	js.Global().Set("pakeUpdate", js.FuncOf(pakeUpdate))
	js.Global().Set("pakeSessionKey", js.FuncOf(pakeSessionKey))
	js.Global().Set("writeWebsocketMessage", js.FuncOf(writeWebsocketMessage))
	js.Global().Set("readWebsocketMessage", js.FuncOf(readWebsocketMessage))
	fmt.Println("Initiated")
	<-c
}
