package croc

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/cihub/seelog"
	humanize "github.com/dustin/go-humanize"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
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
)

var DebugLevel string

// Receive is the async operation to receive a file
func (cr *Croc) startRecipient(forceSend int, serverAddress string, tcpPorts []string, isLocal bool, done chan struct{}, c *websocket.Conn, codephrase string, noPrompt bool, useStdout bool) {
	logger.SetLogLevel(DebugLevel)
	err := cr.receive(forceSend, serverAddress, tcpPorts, isLocal, c, codephrase, noPrompt, useStdout)
	if err != nil {
		if !strings.HasPrefix(err.Error(), "websocket: close 100") {
			fmt.Fprintf(os.Stderr, "\n"+err.Error())
			cr.StateString = err.Error()
			err = errors.Wrap(err, "error in recipient:")
			c.WriteMessage(websocket.TextMessage, []byte(err.Error()))
			time.Sleep(50 * time.Millisecond)
		}
	}
	done <- struct{}{}
}

func (cr *Croc) receive(forceSend int, serverAddress string, tcpPorts []string, isLocal bool, c *websocket.Conn, codephrase string, noPrompt bool, useStdout bool) (err error) {
	var sessionKey []byte
	var transferTime time.Duration
	var hash256 []byte
	var progressFile string
	var resumeFile bool
	var tcpConnections []comm.Comm
	var Q *pake.Pake

	dataChan := make(chan []byte, 1024*1024)
	isConnectedIfUsingTCP := make(chan bool)
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
	spin.Suffix = " connecting..."
	cr.StateString = "Connecting as recipient..."
	spin.Start()
	defer spin.Stop()

	// both parties should have a weak key
	pw := []byte(codephrase)

	// start the reader
	websocketMessages := make(chan WebSocketMessage, 1024)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Debugf("recovered from %s", r)
			}
		}()
		for {
			messageType, message, err := c.ReadMessage()
			websocketMessages <- WebSocketMessage{messageType, message, err}
		}
	}()

	step := 0
	for {
		var websocketMessageMain WebSocketMessage
		// websocketMessageMain = <-websocketMessages
		timeWaitingForMessage := time.Now()
		for {
			done := false
			select {
			case websocketMessageMain = <-websocketMessages:
				done = true
			default:
				time.Sleep(10 * time.Millisecond)
			}
			if done {
				break
			}
			if time.Since(timeWaitingForMessage).Seconds() > 3 && step == 0 {
				return fmt.Errorf("You are trying to receive a file with no sender.")
			}
		}

		messageType := websocketMessageMain.messageType
		message := websocketMessageMain.message
		err := websocketMessageMain.err
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
			spin.Stop()
			spin.Suffix = " performing PAKE..."
			cr.StateString = "Performing PAKE..."
			spin.Start()
			// sender has initiated, sends their initial data
			var initialData models.Initial
			err = json.Unmarshal(message, &initialData)
			if err != nil {
				err = errors.Wrap(err, "incompatible versions of croc")
				return err
			}
			cr.OtherIP = initialData.IPAddress
			log.Debugf("sender IP: %s", cr.OtherIP)

			// check whether the version strings are compatible
			versionStringsOther := strings.Split(initialData.VersionString, ".")
			versionStringsSelf := strings.Split(cr.Version, ".")
			if len(versionStringsOther) == 3 && len(versionStringsSelf) == 3 {
				if versionStringsSelf[0] != versionStringsOther[0] || versionStringsSelf[1] != versionStringsOther[1] {
					return fmt.Errorf("version sender %s is not compatible with recipient %s", cr.Version, initialData.VersionString)
				}
			}

			// initialize the PAKE with the curve sent from the sender
			Q, err = pake.InitCurve(pw, 1, initialData.CurveType, 1*time.Millisecond)
			if err != nil {
				err = errors.Wrap(err, "incompatible curve type")
				return err
			}

			// recipient begins by sending back initial data to sender
			ip := ""
			if isLocal {
				ip = utils.LocalIP()
			} else {
				ip, _ = utils.PublicIP()
			}
			initialData.VersionString = cr.Version
			initialData.IPAddress = ip
			bInitialData, _ := json.Marshal(initialData)
			c.WriteMessage(websocket.BinaryMessage, bInitialData)
		case 1:
			// Q receives u
			log.Debugf("[%d] Q computes k, sends H(k), v back to P", step)
			if err := Q.Update(message); err != nil {
				return fmt.Errorf("Recipient is using wrong code phrase.")
			}

			// Q has the session key now, but we will still check if its valid
			sessionKey, err = Q.SessionKey()
			if err != nil {
				return fmt.Errorf("Recipient is using wrong code phrase.")
			}
			log.Debugf("%x\n", sessionKey)

			// initialize TCP connections if using (possible, but unlikely, race condition)
			go func() {
				log.Debug("initializing TCP connections")
				if !useWebsockets {
					log.Debugf("connecting to server")
					tcpConnections = make([]comm.Comm, len(tcpPorts))
					var wg sync.WaitGroup
					wg.Add(len(tcpPorts))
					for i, tcpPort := range tcpPorts {
						go func(i int, tcpPort string) {
							defer wg.Done()
							log.Debugf("connecting to %d", i)
							var message string
							tcpConnections[i], message, err = connectToTCPServer(utils.SHA256(fmt.Sprintf("%d%x", i, sessionKey)), serverAddress+":"+tcpPort)
							if err != nil {
								log.Error(err)
							}
							if message != "recipient" {
								log.Errorf("got wrong message: %s", message)
							}
						}(i, tcpPort)
					}
					wg.Wait()
					log.Debugf("fully connected")
				}
				isConnectedIfUsingTCP <- true
			}()

			c.WriteMessage(websocket.BinaryMessage, Q.Bytes())
		case 2:
			log.Debugf("[%d] Q recieves H(k) from P", step)
			// check if everything is still kosher with our computed session key
			if err := Q.Update(message); err != nil {
				log.Debug(err)
				return fmt.Errorf("Recipient is using wrong code phrase.")
			}
			c.WriteMessage(websocket.BinaryMessage, []byte("ready"))
		case 3:
			spin.Stop()
			cr.StateString = "Recieving file info..."

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
			err = json.Unmarshal(decryptedFileData, &cr.FileInfo)
			if err != nil {
				return err
			}
			log.Debugf("got file stats: %+v", cr.FileInfo)

			// determine if the file is resuming or not
			progressFile = fmt.Sprintf("%s.progress", cr.FileInfo.SentName)
			overwritingOrReceiving := "Receiving"
			if utils.Exists(cr.FileInfo.Name) || utils.Exists(cr.FileInfo.SentName) {
				overwritingOrReceiving = "Overwriting"
				if utils.Exists(progressFile) {
					overwritingOrReceiving = "Resume receiving"
					resumeFile = true
				}
			}

			// send blocks
			if resumeFile {
				fileWithBlocks, _ := os.Open(progressFile)
				scanner := bufio.NewScanner(fileWithBlocks)
				for scanner.Scan() {
					blocks = append(blocks, strings.TrimSpace(scanner.Text()))
				}
				fileWithBlocks.Close()
			}
			blocksBytes, _ := json.Marshal(blocks)
			// encrypt the block data and send
			encblockBytes := crypt.Encrypt(blocksBytes, sessionKey)

			// wait for TCP connections if using them
			_ = <-isConnectedIfUsingTCP
			c.WriteMessage(websocket.BinaryMessage, encblockBytes.Bytes())

			// prompt user about the file
			fileOrFolder := "file"
			if cr.FileInfo.IsDir {
				fileOrFolder = "folder"
			}
			cr.WindowReceivingString = fmt.Sprintf("%s %s (%s) into: %s",
				overwritingOrReceiving,
				fileOrFolder,
				humanize.Bytes(uint64(cr.FileInfo.Size)),
				cr.FileInfo.Name,
			)
			fmt.Fprintf(os.Stderr, "\r%s\n",
				cr.WindowReceivingString,
			)
			if !noPrompt {
				if "y" != utils.GetInput("ok? (y/N): ") {
					fmt.Fprintf(os.Stderr, "Cancelling request")
					c.WriteMessage(websocket.BinaryMessage, []byte("no"))
					return nil
				}
			}
			if cr.WindowRecipientPrompt {
				// wait until it switches to false
				// the window should then set WindowRecipientAccept
				for {
					if !cr.WindowRecipientPrompt {
						if cr.WindowRecipientAccept {
							break
						} else {
							fmt.Fprintf(os.Stderr, "Cancelling request")
							c.WriteMessage(websocket.BinaryMessage, []byte("no"))
							return nil
						}
					}
					time.Sleep(10 * time.Millisecond)
				}
			}

			// await file
			// erase file if overwriting
			if overwritingOrReceiving == "Overwriting" {
				os.Remove(cr.FileInfo.SentName)
			}
			var f *os.File
			if utils.Exists(cr.FileInfo.SentName) && resumeFile {
				if !useWebsockets {
					f, err = os.OpenFile(cr.FileInfo.SentName, os.O_WRONLY, 0644)
				} else {
					f, err = os.OpenFile(cr.FileInfo.SentName, os.O_APPEND|os.O_WRONLY, 0644)
				}
				if err != nil {
					log.Error(err)
					return err
				}
			} else {
				f, err = os.Create(cr.FileInfo.SentName)
				if err != nil {
					log.Error(err)
					return err
				}
				if !useWebsockets {
					if err = f.Truncate(cr.FileInfo.Size); err != nil {
						log.Error(err)
						return err
					}
				}
			}

			blockSize := 0
			if useWebsockets {
				blockSize = models.WEBSOCKET_BUFFER_SIZE / 8
			} else {
				blockSize = models.TCP_BUFFER_SIZE / 2
			}
			// start the ui for pgoress
			cr.StateString = "Recieving file..."
			bytesWritten := 0
			fmt.Fprintf(os.Stderr, "\nReceiving (<-%s)...\n", cr.OtherIP)
			cr.Bar = progressbar.NewOptions(
				int(cr.FileInfo.Size),
				progressbar.OptionSetRenderBlankState(true),
				progressbar.OptionSetBytes(int(cr.FileInfo.Size)),
				progressbar.OptionSetWriter(os.Stderr),
			)
			cr.Bar.Add((len(blocks) * blockSize))
			finished := make(chan bool)

			go func(finished chan bool, dataChan chan []byte) (err error) {
				// remove previous progress
				var fProgress *os.File
				var progressErr error
				if resumeFile {
					fProgress, progressErr = os.OpenFile(progressFile, os.O_APPEND|os.O_WRONLY, 0644)
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
				blocksToWrite := float64(cr.FileInfo.Size)
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
					decrypted, err := enc.Decrypt(sessionKey, !cr.FileInfo.IsEncrypted)
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
					if cr.FileInfo.IsCompressed && !cr.FileInfo.IsDir {
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
						log.Debugf("wrote %d bytes to location %d (%2.0f/%2.0f)", n, locationToWrite, blocksWritten, blocksToWrite)
					} else {
						// write to file
						n, err = f.Write(decrypted)
						log.Debugf("wrote %d bytes to location %d (%2.0f/%2.0f)", n, bytesWritten, blocksWritten, blocksToWrite)
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
					cr.Bar.Add(n)
					if int64(bytesWritten) == cr.FileInfo.Size || blocksWritten >= blocksToWrite {
						log.Debug("finished", int64(bytesWritten), cr.FileInfo.Size, blocksWritten, blocksToWrite)
						break
					}
				}
				finished <- true
				return
			}(finished, dataChan)

			log.Debug("telling sender i'm ready")
			c.WriteMessage(websocket.BinaryMessage, []byte("ready"))

			startTime := time.Now()
			if useWebsockets {
				for {
					// read from websockets
					websocketMessageData := <-websocketMessages
					if bytes.HasPrefix(websocketMessageData.message, []byte("error")) {
						return fmt.Errorf("%s", websocketMessageData.message)
					}
					if websocketMessageData.messageType != websocket.BinaryMessage {
						continue
					}
					if err != nil {
						log.Error(err)
						return err
					}
					if bytes.Equal(websocketMessageData.message, []byte("magic")) {
						log.Debug("got magic")
						break
					}
					dataChan <- websocketMessageData.message
				}
			} else {
				log.Debugf("starting listening with tcp with %d connections", len(tcpConnections))

				// check to see if any messages are sent
				stopMessageSignal := make(chan bool, 1)
				errorsDuringTransfer := make(chan error, 24)
				go func() {
					for {
						select {
						case sig := <-stopMessageSignal:
							errorsDuringTransfer <- nil
							log.Debugf("got message signal: %+v", sig)
							return
						case wsMessage := <-websocketMessages:
							log.Debugf("got message: %s", wsMessage.message)
							if bytes.HasPrefix(wsMessage.message, []byte("error")) {
								log.Debug("stopping transfer")
								for i := 0; i < len(tcpConnections)+1; i++ {
									errorsDuringTransfer <- fmt.Errorf("%s", wsMessage.message)
								}
								return
							}
						default:
							continue
						}
					}
				}()

				// using TCP
				go func() {
					var wg sync.WaitGroup
					wg.Add(len(tcpConnections))
					for i := range tcpConnections {
						defer func(i int) {
							log.Debugf("closing connection %d", i)
							tcpConnections[i].Close()
						}(i)
						go func(wg *sync.WaitGroup, j int) {
							defer wg.Done()
							for {
								select {
								case _ = <-errorsDuringTransfer:
									log.Debugf("%d got stop", i)
									return
								default:
								}

								log.Debugf("waiting to read on %d", j)
								// read from TCP connection
								message, _, _, err := tcpConnections[j].Read()
								// log.Debugf("message: %s", message)
								if err != nil {
									panic(err)
								}
								if bytes.Equal(message, []byte("magic")) {
									log.Debugf("%d got magic, leaving", j)
									return
								}
								dataChan <- message
							}
						}(&wg, i)
					}
					log.Debug("waiting for tcp goroutines")
					wg.Wait()
					errorsDuringTransfer <- nil
				}()

				// block until this is done

				log.Debug("waiting for error")
				errorDuringTransfer := <-errorsDuringTransfer
				log.Debug("sending stop message signal")
				stopMessageSignal <- true
				if errorDuringTransfer != nil {
					log.Debugf("got error during transfer: %s", errorDuringTransfer.Error())
					return errorDuringTransfer
				}
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
			cr.Bar.Finish()

			// check hash
			hash256, err = utils.HashFile(cr.FileInfo.SentName)
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
				if cr.FileInfo.IsDir {
					err = zipper.UnzipFile(cr.FileInfo.SentName, ".")
					if DebugLevel != "debug" {
						os.Remove(cr.FileInfo.SentName)
					}
				} else {
					err = nil
				}
				if err == nil {
					if useStdout && !cr.FileInfo.IsDir {
						var bFile []byte
						bFile, err = ioutil.ReadFile(cr.FileInfo.SentName)
						if err != nil {
							return err
						}
						os.Stdout.Write(bFile)
						os.Remove(cr.FileInfo.SentName)
					}
					transferRate := float64(cr.FileInfo.Size) / 1000000.0 / transferTime.Seconds()
					transferType := "MB/s"
					if transferRate < 1 {
						transferRate = float64(cr.FileInfo.Size) / 1000.0 / transferTime.Seconds()
						transferType = "kB/s"
					}
					folderOrFile := "file"
					if cr.FileInfo.IsDir {
						folderOrFile = "folder"
					}
					if useStdout {
						cr.FileInfo.Name = "stdout"
					}
					fmt.Fprintf(os.Stderr, "\nReceived %s written to %s (%2.1f %s)", folderOrFile, cr.FileInfo.Name, transferRate, transferType)
					os.Remove(progressFile)
					cr.StateString = fmt.Sprintf("Received %s written to %s (%2.1f %s)", folderOrFile, cr.FileInfo.Name, transferRate, transferType)
				}
				return err
			} else {
				if DebugLevel != "debug" {
					log.Debug("removing corrupted file")
					os.Remove(cr.FileInfo.SentName)
				}
				return errors.New("file corrupted")
			}
		default:
			return fmt.Errorf("unknown step")
		}
		step++
	}
}
