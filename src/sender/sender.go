package sender

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	log "github.com/cihub/seelog"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	"github.com/schollz/croc/src/compress"
	"github.com/schollz/croc/src/crypt"
	"github.com/schollz/croc/src/logger"
	"github.com/schollz/croc/src/models"
	"github.com/schollz/croc/src/utils"
	"github.com/schollz/pake"
	"github.com/schollz/progressbar/v2"
	"github.com/tscholl2/siec"
)

var DebugLevel string

// Send is the async call to send data
func Send(done chan struct{}, c *websocket.Conn, fname string, codephrase string) {
	logger.SetLogLevel(DebugLevel)
	log.Debugf("sending %s", fname)
	err := send(c, fname, codephrase)
	if err != nil {
		if strings.HasPrefix(err.Error(), "websocket: close 100") {
			return
		}
		log.Error(err)
	}
	done <- struct{}{}
}

func send(c *websocket.Conn, fname string, codephrase string) (err error) {
	// check that the file exists
	f, err := os.Open(fname)
	if err != nil {
		return
	}
	fstat, err := f.Stat()
	if err != nil {
		return err
	}
	fstats := models.FileStats{fstat.Name(), fstat.Size(), fstat.ModTime(), fstat.IsDir()}
	if fstats.IsDir {
		// zip the directory

	}

	// get ready to generate session key
	var sessionKey []byte

	// pick an elliptic curve
	curve := siec.SIEC255()
	// both parties should have a weak key
	pw := []byte(codephrase)
	// initialize sender P ("0" indicates sender)
	P, err := pake.Init(pw, 0, curve, 100*time.Millisecond)
	if err != nil {
		return
	}

	step := 0
	for {
		messageType, message, errRead := c.ReadMessage()
		if errRead != nil {
			return errRead
		}
		if messageType == websocket.PongMessage || messageType == websocket.PingMessage {
			continue
		}
		log.Debugf("got %d: %s", messageType, message)
		switch step {
		case 0:
			log.Debugf("[%d] first, P sends u to Q", step)
			c.WriteMessage(websocket.BinaryMessage, P.Bytes())
		case 1:
			// P recieves H(k),v from Q
			log.Debugf("[%d] P computes k, H(k), sends H(k) to Q", step)
			if err := P.Update(message); err != nil {
				return err
			}
			c.WriteMessage(websocket.BinaryMessage, P.Bytes())
			sessionKey, _ = P.SessionKey()
			// check(err)
			log.Debugf("%x\n", sessionKey)
		case 2:
			log.Debugf("[%d] recipient declares readiness for file info", step)
			if !bytes.Equal(message, []byte("ready")) {
				return errors.New("recipient refused file")
			}
			fstatsBytes, err := json.Marshal(fstats)
			if err != nil {
				return err
			}
			log.Debugf("%s\n", fstatsBytes)
			c.WriteMessage(websocket.BinaryMessage, fstatsBytes)
		case 3:
			log.Debugf("[%d] recipient declares readiness for file data", step)
			if !bytes.Equal(message, []byte("ready")) {
				return errors.New("recipient refused file")
			}
			// send file, compure hash simultaneously
			buffer := make([]byte, 1024*512)
			bar := progressbar.NewOptions(
				int(fstats.Size),
				progressbar.OptionSetRenderBlankState(true),
				progressbar.OptionSetBytes(int(fstats.Size)),
			)
			for {
				bytesread, err := f.Read(buffer)
				bar.Add(bytesread)
				if bytesread > 0 {
					// do compression
					compressedBytes := compress.Compress(buffer[:bytesread])
					// compressedBytes := buffer[:bytesread]

					// do encryption
					enc := crypt.Encrypt(compressedBytes, sessionKey, true)
					encBytes, err := json.Marshal(enc)
					if err != nil {
						return err
					}

					// send message
					err = c.WriteMessage(websocket.BinaryMessage, encBytes)
					if err != nil {
						err = errors.Wrap(err, "problem writing message")
						return err
					}
					// wait for ok
					c.ReadMessage()
				}
				if err != nil {
					if err != io.EOF {
						fmt.Println(err)
					}
					break
				}
			}

			bar.Finish()
			log.Debug("send hash to finish file")
			fileHash, err := utils.HashFile(fname)
			if err != nil {
				return err
			}
			c.WriteMessage(websocket.TextMessage, fileHash)
		case 4:
			log.Debugf("[%d] determing whether it went ok", step)
			if bytes.Equal(message, []byte("ok")) {
				log.Debug("file transfered successfully")
				return nil
			} else {
				return errors.New("file not transfered succesfully")
			}
		default:
			return fmt.Errorf("unknown step")
		}
		step++
	}
}
