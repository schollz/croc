package croc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/schollz/croc/v11/src/message"
	"github.com/schollz/croc/v11/src/utils"
)

const (
	defaultHashAlgorithm     = "xxhash"
	progressiveHashOption    = "imohash"
	progressiveHashAlgorithm = "imohash-v2"
)

func (c *Client) progressiveHashActive() bool {
	return c.peerProgressiveHash && c.Options.HashAlgorithm == progressiveHashAlgorithm
}

func (c *Client) processMessageFilePrepared(m message.Message) error {
	if c.Options.IsSender || !c.progressiveHashActive() {
		return errors.New("unexpected file-prepared message")
	}
	var prepared filePrepared
	if err := json.Unmarshal(m.Bytes, &prepared); err != nil {
		return err
	}
	if prepared.Index < 0 || prepared.Index >= len(c.FilesToTransfer) {
		return fmt.Errorf("invalid prepared file index %d", prepared.Index)
	}
	if len(prepared.Hash) == 0 {
		return errors.New("prepared file digest is empty")
	}
	file := &c.FilesToTransfer[prepared.Index]
	if file.Prepared {
		return fmt.Errorf("duplicate prepared file index %d", prepared.Index)
	}
	file.Hash = append([]byte(nil), prepared.Hash...)
	file.IsCompressed = prepared.IsCompressed
	file.Prepared = true
	return nil
}

func (c *Client) beginExactHash(index int, filename string) error {
	c.exactHashMu.Lock()
	if c.exactHashPending >= 0 {
		c.exactHashMu.Unlock()
		return fmt.Errorf("exact hash already pending for file %d", c.exactHashPending)
	}
	c.exactHashPending = index
	c.exactHashLocal = nil
	c.exactHashMu.Unlock()

	if err := message.Send(c.connection(0), c.Key, message.Message{
		Type: message.TypeExactHashRequest,
		Num:  index,
	}); err != nil {
		c.clearExactHashPending(index)
		return err
	}
	hash, err := utils.HashFileCtx(c.stop.ctx, filename, defaultHashAlgorithm, !c.Options.SendingText)
	if err != nil {
		c.clearExactHashPending(index)
		return err
	}
	c.exactHashMu.Lock()
	if c.exactHashPending == index {
		c.exactHashLocal = hash
	}
	c.exactHashMu.Unlock()
	return nil
}

func (c *Client) clearExactHashPending(index int) {
	c.exactHashMu.Lock()
	defer c.exactHashMu.Unlock()
	if c.exactHashPending == index {
		c.exactHashPending = -1
		c.exactHashLocal = nil
	}
}

func (c *Client) exactHashDecision(index int) (known, matches, pending bool) {
	c.exactHashMu.Lock()
	defer c.exactHashMu.Unlock()
	matches, known = c.exactHashResults[index]
	if known {
		delete(c.exactHashResults, index)
	}
	pending = c.exactHashPending == index
	return
}

func (c *Client) processExactHashRequest(m message.Message) error {
	if !c.Options.IsSender || !c.progressiveHashActive() {
		return errors.New("unexpected exact hash request")
	}
	index := m.Num
	if index < 0 || index >= len(c.FilesToTransfer) || !c.FilesToTransfer[index].Prepared {
		return fmt.Errorf("invalid exact hash request index %d", index)
	}
	if err := c.validateSourceUnchanged(index); err != nil {
		return err
	}
	filename := sourceFilePath(c.FilesToTransfer[index])
	hash, err := c.stop.hash(filename, defaultHashAlgorithm, c.FilesToTransfer[index].Size > 1e7)
	if err != nil {
		return err
	}
	if err := c.validateSourceUnchanged(index); err != nil {
		return err
	}
	return message.Send(c.connection(0), c.Key, message.Message{
		Type:  message.TypeExactHashResult,
		Num:   index,
		Bytes: hash,
	})
}

func (c *Client) processExactHashResult(m message.Message) error {
	if c.Options.IsSender || !c.progressiveHashActive() {
		return errors.New("unexpected exact hash result")
	}
	c.exactHashMu.Lock()
	defer c.exactHashMu.Unlock()
	if c.exactHashPending != m.Num || c.exactHashLocal == nil {
		return fmt.Errorf("unexpected exact hash result index %d", m.Num)
	}
	c.exactHashResults[m.Num] = bytes.Equal(c.exactHashLocal, m.Bytes)
	c.exactHashPending = -1
	c.exactHashLocal = nil
	return nil
}
