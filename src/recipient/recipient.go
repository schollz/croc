package recipient

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	humanize "github.com/dustin/go-humanize"

	log "github.com/cihub/seelog"
	"github.com/gorilla/websocket"
	"github.com/schollz/croc/src/comm"
	"github.com/schollz/croc/src/compress"
	"github.com/schollz/croc/src/crypt"
	"github.com/schollz/croc/src/logger"
	"github.com/schollz/croc/src/models"
	"github.com/schollz/croc/src/utils"
	"github.com/schollz/croc/src/zipper"
	"github.com/schollz/pake"
	"github.com/schollz/progressbar/v2"
	"github.com/schollz/spinner"
	"github.com/tscholl2/siec"
)

var DebugLevel string

// Receive is the async operation to receive a file
func Receive(forceSend int, serverAddress string, tcpPorts []string, isLocal bool, done chan struct{}, c *websocket.Conn, codephrase string, noPrompt bool, useStdout bool) {
	logger.SetLogLevel(DebugLevel)
	err := receive(forceSend, serverAddress, tcpPorts, isLocal, c, codephrase, noPrompt, useStdout)
	if err != nil {
		if !strings.HasPrefix(err.Error(), "websocket: close 100") {
			fmt.Fprintf(os.Stderr, "\n"+err.Error())
		}
	}
	done <- struct{}{}
}

func receive(forceSend int, serverAddress string, tcpPorts []string, isLocal bool, c *websocket.Conn, codephrase string, noPrompt bool, useStdout bool) (err error) {
	var fstats models.FileStats
	var sessionKey []byte
	var transferTime time.Duration
	var hash256 []byte
	var otherIP string
	var progressFile string
	var resumeFile bool
	var tcpConnections []comm.Comm
	dataChan := make(chan []byte, 1024*1024)
	blocks := []string{}

	useWebsockets := true
	switch forceSend {
	case 0:
		if !isLocal {
			useWebsockets = false
		}
	case 1:
		useWebsockets = true
	case 2:
		useWebsockets = false
	}

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
	Q, err := pake.Init(pw, 1, curve, 1*time.Millisecond)
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
		if messageType == websocket.TextMessage && bytes.Equal(message, []byte("interrupt")) {
			return errors.New("\rinterrupted by other party")
		}

		log.Debugf("got %d: %s", messageType, message)
		switch step {
		case 0:
			// sender has initiated, sends their ip address
			otherIP = string(message)
			log.Debugf("sender IP: %s", otherIP)

			// recipient begins by sending address
			ip := ""
			if isLocal {
				ip = utils.LocalIP()
			} else {
				ip, _ = utils.PublicIP()
			}
			c.WriteMessage(websocket.BinaryMessage, []byte(ip))
		case 1:

			// Q receives u
			log.Debugf("[%d] Q computes k, sends H(k), v back to P", step)
			if err := Q.Update(message); err != nil {
				return err
			}
			c.WriteMessage(websocket.BinaryMessage, Q.Bytes())
		case 2:
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
		case 3:
			spin.Stop()

			// unmarshal the file info
			log.Debugf("[%d] recieve file info", step)
			// do decryption on the file stats
			enc, err := crypt.FromBytes(message)
			if err != nil {
				return err
			}
			decryptedFileData, err := enc.Decrypt(sessionKey)
			if err != nil {
				return err
			}
			err = json.Unmarshal(decryptedFileData, &fstats)
			if err != nil {
				return err
			}
			log.Debugf("got file stats: %+v", fstats)

			// prompt user if its okay to receive file
			progressFile = fmt.Sprintf("%s.progress", fstats.SentName)
			overwritingOrReceiving := "Receiving"
			if utils.Exists(fstats.Name) || utils.Exists(fstats.SentName) {
				overwritingOrReceiving = "Overwriting"
				if utils.Exists(progressFile) {
					overwritingOrReceiving = "Resume receiving"
					resumeFile = true
				}
			}
			fileOrFolder := "file"
			if fstats.IsDir {
				fileOrFolder = "folder"
			}
			fmt.Fprintf(os.Stderr, "\r%s %s (%s) into: %s\n",
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

			// connect to TCP to receive file
			if !useWebsockets {
				log.Debugf("connecting to server")
				tcpConnections = make([]comm.Comm, len(tcpPorts))
				for i, tcpPort := range tcpPorts {
					tcpConnections[i], err = connectToTCPServer(utils.SHA256(fmt.Sprintf("%d%x", i, sessionKey)), serverAddress+":"+tcpPort)
					if err != nil {
						log.Error(err)
						return err
					}
					defer tcpConnections[i].Close()
				}
				log.Debugf("fully connected")
			}

			// await file
			var f *os.File
			if utils.Exists(fstats.SentName) && resumeFile {
				if !useWebsockets {
					f, err = os.OpenFile(fstats.SentName, os.O_WRONLY, 0644)
				} else {
					f, err = os.OpenFile(fstats.SentName, os.O_APPEND, 0644)
				}
				if err != nil {
					log.Error(err)
					return err
				}
			} else {
				f, err = os.Create(fstats.SentName)
				if err != nil {
					log.Error(err)
					return err
				}
				if !useWebsockets {
					if err = f.Truncate(fstats.Size); err != nil {
						log.Error(err)
						return err
					}
				}
			}

			// append the previous blocks if there was progress previously
			if resumeFile {
				file, _ := os.Open(progressFile)
				scanner := bufio.NewScanner(file)
				for scanner.Scan() {
					blocks = append(blocks, strings.TrimSpace(scanner.Text()))
				}
				file.Close()
			}
			blocksBytes, _ := json.Marshal(blocks)

			blockSize := 0
			if useWebsockets {
				blockSize = models.WEBSOCKET_BUFFER_SIZE / 8
			} else {
				blockSize = models.TCP_BUFFER_SIZE / 2
			}

			// start the ui for pgoress
			bytesWritten := 0
			fmt.Fprintf(os.Stderr, "\nReceiving (<-%s)...\n", otherIP)
			bar := progressbar.NewOptions(
				int(fstats.Size),
				progressbar.OptionSetRenderBlankState(true),
				progressbar.OptionSetBytes(int(fstats.Size)),
				progressbar.OptionSetWriter(os.Stderr),
			)
			bar.Add((len(blocks) * blockSize))
			finished := make(chan bool)

			go func(finished chan bool, dataChan chan []byte) (err error) {
				// remove previous progress
				var fProgress *os.File
				var progressErr error
				if resumeFile {
					fProgress, progressErr = os.OpenFile(progressFile, os.O_APPEND, 0644)
					bytesWritten = len(blocks) * blockSize
				} else {
					os.Remove(progressFile)
					fProgress, progressErr = os.Create(progressFile)
				}
				if progressErr != nil {
					panic(progressErr)
				}
				defer fProgress.Close()

				blocksWritten := 0.0
				blocksToWrite := float64(fstats.Size)
				if useWebsockets {
					blocksToWrite = blocksToWrite/float64(models.WEBSOCKET_BUFFER_SIZE/8) - float64(len(blocks))
				} else {
					blocksToWrite = blocksToWrite/float64(models.TCP_BUFFER_SIZE/2) - float64(len(blocks))
				}
				for {
					message := <-dataChan
					// do decryption
					var enc crypt.Encryption
					err = json.Unmarshal(message, &enc)
					if err != nil {
						// log.Errorf("%s: [%s] [%+v] (%d/%d) %+v", err.Error(), message, message, len(message), numBytes, bs)
						log.Error(err)
						return err
					}
					decrypted, err := enc.Decrypt(sessionKey, !fstats.IsEncrypted)
					if err != nil {
						log.Error(err)
						return err
					}

					// get location if TCP
					var locationToWrite int
					if !useWebsockets {
						pieces := bytes.SplitN(decrypted, []byte("-"), 2)
						decrypted = pieces[1]
						locationToWrite, _ = strconv.Atoi(string(pieces[0]))
					}

					// do decompression
					if fstats.IsCompressed && !fstats.IsDir {
						decrypted = compress.Decompress(decrypted)
					}

					var n int
					if !useWebsockets {
						if err != nil {
							log.Error(err)
							return err
						}
						n, err = f.WriteAt(decrypted, int64(locationToWrite))
						fProgress.WriteString(fmt.Sprintf("%d\n", locationToWrite))
						log.Debugf("writing to location %d (%2.0f/%2.0f)", locationToWrite, blocksWritten, blocksToWrite)
					} else {
						// write to file
						n, err = f.Write(decrypted)
						log.Debugf("writing to location %d (%2.0f/%2.0f)", bytesWritten, blocksWritten, blocksToWrite)
						fProgress.WriteString(fmt.Sprintf("%d\n", bytesWritten))
					}

					if err != nil {
						log.Error(err)
						return err
					}
					// update the bytes written
					bytesWritten += n
					blocksWritten += 1.0
					// update the progress bar
					bar.Add(n)
					if int64(bytesWritten) == fstats.Size || blocksWritten >= blocksToWrite {
						log.Debug("finished")
						break
					}
				}
				finished <- true
				return
			}(finished, dataChan)

			log.Debug("telling sender i'm ready")
			c.WriteMessage(websocket.BinaryMessage, append([]byte("ready"), blocksBytes...))

			startTime := time.Now()
			if useWebsockets {
				for {
					var messageType int
					// read from websockets
					messageType, message, err = c.ReadMessage()
					if messageType != websocket.BinaryMessage {
						continue
					}
					if err != nil {
						log.Error(err)
						return err
					}
					if bytes.Equal(message, []byte("magic")) {
						log.Debug("got magic")
						break
					}
					select {
					case dataChan <- message:
					default:
						log.Debug("blocked")
						// no message sent
						// block
						dataChan <- message
					}
				}
			} else {
				log.Debugf("starting listening with tcp with %d connections", len(tcpConnections))
				// using TCP
				var wg sync.WaitGroup
				wg.Add(len(tcpConnections))
				for i := range tcpConnections {
					go func(wg *sync.WaitGroup, tcpConnection comm.Comm) {
						defer wg.Done()
						for {
							// read from TCP connection
							message, _, _, err = tcpConnection.Read()
							// log.Debugf("message: %s", message)
							if err != nil {
								log.Error(err)
								return
							}
							if bytes.Equal(message, []byte("magic")) {
								log.Debug("got magic")
								return
							}
							select {
							case dataChan <- message:
							default:
								log.Debug("blocked")
								// no message sent
								// block
								dataChan <- message
							}
						}
					}(&wg, tcpConnections[i])
				}
				wg.Wait()
			}

			_ = <-finished
			log.Debug("telling sender i'm done")
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
		case 4:
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
					os.Remove(progressFile)
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

func connectToTCPServer(room string, address string) (com comm.Comm, err error) {
	log.Debugf("recipient connecting to %s", address)
	connection, err := net.Dial("tcp", address)
	if err != nil {
		return
	}
	connection.SetReadDeadline(time.Now().Add(3 * time.Hour))
	connection.SetDeadline(time.Now().Add(3 * time.Hour))
	connection.SetWriteDeadline(time.Now().Add(3 * time.Hour))

	com = comm.New(connection)
	log.Debug("waiting for server contact")
	ok, err := com.Receive()
	if err != nil {
		return
	}
	log.Debugf("[%s] server says: %s", address, ok)

	err = com.Send(room)
	if err != nil {
		return
	}
	ok, err = com.Receive()
	log.Debugf("[%s] server says: %s", address, ok)
	if err != nil {
		return
	}
	if ok != "recipient" {
		err = errors.New(ok)
	}
	return
}
