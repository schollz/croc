package recipient

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	humanize "github.com/dustin/go-humanize"

	log "github.com/cihub/seelog"
	"github.com/gorilla/websocket"
	"github.com/schollz/croc/src/compress"
	"github.com/schollz/croc/src/crypt"
	"github.com/schollz/croc/src/logger"
	"github.com/schollz/croc/src/models"
	"github.com/schollz/croc/src/utils"
	"github.com/schollz/croc/src/zipper"
	"github.com/schollz/pake"
	"github.com/schollz/progressbar/v2"
	"github.com/tscholl2/siec"
)

var DebugLevel string

// Receive is the async operation to receive a file
func Receive(done chan struct{}, c *websocket.Conn, codephrase string, noPrompt bool, useStdout bool) {
	logger.SetLogLevel(DebugLevel)
	err := receive(c, codephrase, noPrompt, useStdout)
	if err != nil {
		if strings.HasPrefix(err.Error(), "websocket: close 100") {
			return
		}
		log.Error(err)
	}
	done <- struct{}{}
}

func receive(c *websocket.Conn, codephrase string, noPrompt bool, useStdout bool) (err error) {
	var fstats models.FileStats
	var sessionKey []byte
	var transferTime time.Duration
	var hash256 []byte

	// start a spinner
	spin := spinner.New(spinner.CharSets[9], 100*time.Millisecond)
	spin.Writer = os.Stderr
	spin.Suffix = " performing PAKE..."
	spin.Start()

	// pick an elliptic curve
	curve := siec.SIEC255()
	// both parties should have a weak key
	pw := []byte(codephrase)

	// initialize recipient Q ("1" indicates recipient)
	Q, err := pake.Init(pw, 1, curve, 100*time.Millisecond)
	if err != nil {
		return
	}

	step := 0
	for {
		messageType, message, err := c.ReadMessage()
		if err != nil {
			return err
		}
		if messageType == websocket.PongMessage || messageType == websocket.PingMessage {
			continue
		}

		log.Debugf("got %d: %s", messageType, message)
		switch step {
		case 0:
			// Q receives u
			log.Debugf("[%d] Q computes k, sends H(k), v back to P", step)
			if err := Q.Update(message); err != nil {
				return err
			}
			c.WriteMessage(websocket.BinaryMessage, Q.Bytes())
		case 1:
			log.Debugf("[%d] Q recieves H(k) from P", step)
			if err := Q.Update(message); err != nil {
				return err
			}

			sessionKey, err = Q.SessionKey()
			if err != nil {
				return err
			}
			log.Debugf("%x\n", sessionKey)
			c.WriteMessage(websocket.BinaryMessage, []byte("ready"))
		case 2:
			spin.Stop()

			// unmarshal the file info
			log.Debugf("[%d] recieve file info", step)
			err = json.Unmarshal(message, &fstats)
			if err != nil {
				return err
			}

			// prompt user if its okay to receive file
			overwritingOrReceiving := "Receiving"
			if utils.Exists(fstats.Name) {
				overwritingOrReceiving = "Overwriting"
			}
			fileOrFolder := "file"
			if fstats.IsDir {
				fileOrFolder = "folder"
			}
			fmt.Fprintf(os.Stderr, "%s %s (%s) into: %s\n",
				overwritingOrReceiving,
				fileOrFolder,
				humanize.Bytes(uint64(fstats.Size)),
				fstats.Name,
			)
			if !noPrompt {
				if "y" != utils.GetInput("ok? (y/N): ") {
					fmt.Fprintf(os.Stderr, "cancelling request")
					c.WriteMessage(websocket.BinaryMessage, []byte("no"))
					return nil
				}
			}

			// await file
			f, err := os.Create(fstats.SentName)
			if err != nil {
				log.Error(err)
				return err
			}
			bytesWritten := 0
			fmt.Fprintf(os.Stderr, "\nReceiving...\n")
			bar := progressbar.NewOptions(
				int(fstats.Size),
				progressbar.OptionSetRenderBlankState(true),
				progressbar.OptionSetBytes(int(fstats.Size)),
				progressbar.OptionSetWriter(os.Stderr),
			)
			c.WriteMessage(websocket.BinaryMessage, []byte("ready"))
			startTime := time.Now()
			for {
				messageType, message, err := c.ReadMessage()
				if err != nil {
					return err
				}
				if messageType != websocket.BinaryMessage {
					continue
				}

				// // tell the sender that we recieved this packet
				// c.WriteMessage(websocket.BinaryMessage, []byte("ok"))

				// do decryption
				var enc crypt.Encryption
				err = json.Unmarshal(message, &enc)
				if err != nil {
					return err
				}
				decrypted, err := enc.Decrypt(sessionKey, !fstats.IsEncrypted)
				if err != nil {
					return err
				}

				// do decompression
				if fstats.IsCompressed && !fstats.IsDir {
					decrypted = compress.Decompress(decrypted)
				}

				// write to file
				n, err := f.Write(decrypted)
				if err != nil {
					return err
				}
				// update the bytes written
				bytesWritten += n
				// update the progress bar
				bar.Add(n)

				if int64(bytesWritten) == fstats.Size {
					break
				}
			}

			c.WriteMessage(websocket.BinaryMessage, []byte("done"))
			// we are finished
			transferTime = time.Since(startTime)

			// close file
			err = f.Close()
			if err != nil {
				return err
			}

			// finish bar
			bar.Finish()

			// check hash
			hash256, err = utils.HashFile(fstats.SentName)
			if err != nil {
				log.Error(err)
				return err
			}
			// tell the sender the hash so they can quit
			c.WriteMessage(websocket.BinaryMessage, append([]byte("hash:"), hash256...))
		case 3:
			// receive the hash from the sender so we can check it and quit
			log.Debugf("got hash: %x", message)
			if bytes.Equal(hash256, message) {
				// open directory
				if fstats.IsDir {
					err = zipper.UnzipFile(fstats.SentName, ".")
					if DebugLevel != "debug" {
						os.Remove(fstats.SentName)
					}
				} else {
					err = nil
				}
				if err == nil {
					if useStdout && !fstats.IsDir {
						var bFile []byte
						bFile, err = ioutil.ReadFile(fstats.SentName)
						if err != nil {
							return err
						}
						os.Stdout.Write(bFile)
						os.Remove(fstats.SentName)
					}
					transferRate := float64(fstats.Size) / 1000000.0 / transferTime.Seconds()
					transferType := "MB/s"
					if transferRate < 1 {
						transferRate = float64(fstats.Size) / 1000.0 / transferTime.Seconds()
						transferType = "kB/s"
					}
					folderOrFile := "file"
					if fstats.IsDir {
						folderOrFile = "folder"
					}
					if useStdout {
						fstats.Name = "stdout"
					}
					fmt.Fprintf(os.Stderr, "\nReceived %s written to %s (%2.1f %s)\n", folderOrFile, fstats.Name, transferRate, transferType)
				}
				return err
			} else {
				if DebugLevel != "debug" {
					log.Debug("removing corrupted file")
					os.Remove(fstats.SentName)
				}
				return errors.New("file corrupted")
			}

		default:
			return fmt.Errorf("unknown step")
		}
		step++
	}
}
