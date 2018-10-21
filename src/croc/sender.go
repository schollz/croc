package croc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/cihub/seelog"
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
	progressbar "github.com/schollz/progressbar/v2"
	"github.com/schollz/spinner"
)

// Send is the async call to send data
func (cr *Croc) startSender(forceSend int, serverAddress string, tcpPorts []string, isLocal bool, done chan struct{}, c *websocket.Conn, fname string, codephrase string, useCompression bool, useEncryption bool) {
	logger.SetLogLevel(DebugLevel)
	log.Debugf("sending %s", fname)
	err := cr.send(forceSend, serverAddress, tcpPorts, isLocal, c, fname, codephrase, useCompression, useEncryption)
	if err != nil {
		if !strings.HasPrefix(err.Error(), "websocket: close 100") {
			fmt.Fprintf(os.Stderr, "\n"+err.Error())
			err = errors.Wrap(err, "error in sender:")
			c.WriteMessage(websocket.TextMessage, []byte(err.Error()))
			time.Sleep(50 * time.Millisecond)
			cr.StateString = err.Error()
		}
	}

	done <- struct{}{}
}

func (cr *Croc) send(forceSend int, serverAddress string, tcpPorts []string, isLocal bool, c *websocket.Conn, fname string, codephrase string, useCompression bool, useEncryption bool) (err error) {
	var f *os.File
	defer f.Close() // ignore the error if it wasn't opened :(
	var fileHash []byte
	var startTransfer time.Time
	var tcpConnections []comm.Comm
	blocksToSkip := make(map[int64]struct{})
	isConnectedIfUsingTCP := make(chan bool)

	type DataChan struct {
		b                []byte
		currentPostition int64
		bytesRead        int
		err              error
	}
	dataChan := make(chan DataChan, 1024*1024)
	defer close(dataChan)

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

	fileReady := make(chan error)

	// normalize the file name
	fname, err = filepath.Abs(fname)
	if err != nil {
		return err
	}
	_, filename := filepath.Split(fname)

	// get ready to generate session key
	var sessionKey []byte

	// start a spinner
	spin := spinner.New(spinner.CharSets[9], 100*time.Millisecond)
	spin.Writer = os.Stderr
	defer spin.Stop()

	// both parties should have a weak key
	pw := []byte(codephrase)
	// initialize sender P ("0" indicates sender)
	P, err := pake.InitCurve(pw, 0, cr.CurveType, 1*time.Millisecond)
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
		if messageType == websocket.TextMessage && bytes.Equal(message, []byte("interrupt")) {
			return errors.New("\rinterrupted by other party")
		}
		if messageType == websocket.TextMessage && bytes.HasPrefix(message, []byte("err")) {
			return errors.New("\r" + string(message))
		}
		log.Debugf("got %d: %s", messageType, message)
		switch step {
		case 0:
			// sender initiates communication
			ip := ""
			if isLocal {
				ip = utils.LocalIP()
			} else {
				ip, _ = utils.PublicIP()
			}

			initialData := models.Initial{
				CurveType:     cr.CurveType,
				IPAddress:     ip,
				VersionString: cr.Version, // version should match
			}
			bInitialData, _ := json.Marshal(initialData)
			// send the initial data
			c.WriteMessage(websocket.BinaryMessage, bInitialData)
		case 1:
			// first receive the initial data from the recipient
			var initialData models.Initial
			err = json.Unmarshal(message, &initialData)
			if err != nil {
				err = errors.Wrap(err, "incompatible versions of croc")
				return
			}
			cr.OtherIP = initialData.IPAddress
			log.Debugf("recipient IP: %s", cr.OtherIP)

			go func() {
				// recipient might want file! start gathering information about file
				fstat, err := os.Stat(fname)
				if err != nil {
					fileReady <- err
					return
				}
				cr.FileInfo = models.FileStats{
					Name:         filename,
					Size:         fstat.Size(),
					ModTime:      fstat.ModTime(),
					IsDir:        fstat.IsDir(),
					SentName:     fstat.Name(),
					IsCompressed: useCompression,
					IsEncrypted:  useEncryption,
				}
				if cr.FileInfo.IsDir {
					// zip the directory
					cr.FileInfo.SentName, err = zipper.ZipFile(fname, true)
					if err != nil {
						log.Error(err)
						fileReady <- err
						return
					}
					fname = cr.FileInfo.SentName

					fstat, err := os.Stat(fname)
					if err != nil {
						fileReady <- err
						return
					}
					// get new size
					cr.FileInfo.Size = fstat.Size()
				}

				// open the file
				f, err = os.Open(fname)
				if err != nil {
					fileReady <- err
					return
				}
				fileReady <- nil

			}()

			// send pake data
			log.Debugf("[%d] first, P sends u to Q", step)
			c.WriteMessage(websocket.BinaryMessage, P.Bytes())
			// start PAKE spinnner
			spin.Suffix = " performing PAKE..."
			cr.StateString = "Performing PAKE..."
			spin.Start()
		case 2:
			// P recieves H(k),v from Q
			log.Debugf("[%d] P computes k, H(k), sends H(k) to Q", step)
			if err := P.Update(message); err != nil {
				return err
			}
			c.WriteMessage(websocket.BinaryMessage, P.Bytes())
			sessionKey, _ = P.SessionKey()
			// check(err)
			log.Debugf("%x\n", sessionKey)

			// wait for readiness
			spin.Stop()
			spin.Suffix = " waiting for recipient ok..."
			cr.StateString = "Waiting for recipient ok...."
			spin.Start()
		case 3:
			log.Debugf("[%d] recipient declares readiness for file info", step)
			if !bytes.HasPrefix(message, []byte("ready")) {
				return errors.New("recipient refused file")
			}

			err = <-fileReady // block until file is ready
			if err != nil {
				return err
			}
			fstatsBytes, err := json.Marshal(cr.FileInfo)
			if err != nil {
				return err
			}

			// encrypt the file meta data
			enc := crypt.Encrypt(fstatsBytes, sessionKey)
			// send the file meta data
			c.WriteMessage(websocket.BinaryMessage, enc.Bytes())
		case 4:
			log.Debugf("[%d] recipient gives blocks", step)
			// recipient sends blocks, and sender does not send anything back
			// determine if any blocks were sent to skip
			enc, err := crypt.FromBytes(message)
			if err != nil {
				log.Error(err)
				return err
			}
			decrypted, err := enc.Decrypt(sessionKey)
			if err != nil {
				err = errors.Wrap(err, "could not decrypt blocks with session key")
				log.Error(err)
				return err
			}

			var blocks []string
			errBlocks := json.Unmarshal(decrypted, &blocks)
			if errBlocks == nil {
				for _, block := range blocks {
					blockInt64, errBlock := strconv.Atoi(block)
					if errBlock == nil {
						blocksToSkip[int64(blockInt64)] = struct{}{}
					}
				}
			}
			log.Debugf("found blocks: %+v", blocksToSkip)

			// connect to TCP in background
			tcpConnections = make([]comm.Comm, len(tcpPorts))
			go func() {
				if !useWebsockets {
					log.Debugf("connecting to server")
					for i, tcpPort := range tcpPorts {
						log.Debugf("connecting to %s on connection %d", tcpPort, i)
						var message string
						tcpConnections[i], message, err = connectToTCPServer(utils.SHA256(fmt.Sprintf("%d%x", i, sessionKey)), serverAddress+":"+tcpPort)
						if err != nil {
							log.Error(err)
						}
						if message != "sender" {
							log.Errorf("got wrong message: %s", message)
						}
					}
				}
				isConnectedIfUsingTCP <- true
			}()

			// start loading the file into memory
			// start streaming encryption/compression
			if cr.FileInfo.IsDir {
				// remove file if zipped
				defer os.Remove(cr.FileInfo.SentName)
			}
			go func(dataChan chan DataChan) {
				var buffer []byte
				if useWebsockets {
					buffer = make([]byte, models.WEBSOCKET_BUFFER_SIZE/8)
				} else {
					buffer = make([]byte, models.TCP_BUFFER_SIZE/2)
				}

				currentPostition := int64(0)
				for {
					bytesread, err := f.Read(buffer)
					if bytesread > 0 {
						if _, ok := blocksToSkip[currentPostition]; ok {
							log.Debugf("skipping the sending of block %d", currentPostition)
							currentPostition += int64(bytesread)
							continue
						}

						// do compression
						var compressedBytes []byte
						if useCompression && !cr.FileInfo.IsDir {
							compressedBytes = compress.Compress(buffer[:bytesread])
						} else {
							compressedBytes = buffer[:bytesread]
						}

						// if using TCP, prepend the location to write the data to in the resulting file
						if !useWebsockets {
							compressedBytes = append([]byte(fmt.Sprintf("%d-", currentPostition)), compressedBytes...)
						}

						// do encryption
						enc := crypt.Encrypt(compressedBytes, sessionKey, !useEncryption)
						encBytes, err := json.Marshal(enc)
						if err != nil {
							dataChan <- DataChan{
								b:         nil,
								bytesRead: 0,
								err:       err,
							}
							return
						}

						dataChan <- DataChan{
							b:         encBytes,
							bytesRead: bytesread,
							err:       nil,
						}
						currentPostition += int64(bytesread)
					}
					if err != nil {
						if err != io.EOF {
							log.Error(err)
						}
						break
					}
				}
				// finish
				log.Debug("sending magic")
				dataChan <- DataChan{
					b:         []byte("magic"),
					bytesRead: 0,
					err:       nil,
				}
				if !useWebsockets {
					log.Debug("sending extra magic to %d others", len(tcpPorts)-1)
					for i := 0; i < len(tcpPorts)-1; i++ {
						log.Debug("sending magic")
						dataChan <- DataChan{
							b:         []byte("magic"),
							bytesRead: 0,
							err:       nil,
						}
					}
				}
			}(dataChan)

		case 5:
			spin.Stop()

			log.Debugf("[%d] recipient declares readiness for file data", step)
			if !bytes.HasPrefix(message, []byte("ready")) {
				return errors.New("recipient refused file")
			}
			cr.StateString = "Transfer in progress..."
			fmt.Fprintf(os.Stderr, "\rSending (->%s)...\n", cr.OtherIP)
			// send file, compure hash simultaneously
			startTransfer = time.Now()

			blockSize := 0
			if useWebsockets {
				blockSize = models.WEBSOCKET_BUFFER_SIZE / 8
			} else {
				blockSize = models.TCP_BUFFER_SIZE / 2
			}
			cr.Bar = progressbar.NewOptions(
				int(cr.FileInfo.Size),
				progressbar.OptionSetRenderBlankState(true),
				progressbar.OptionSetBytes(int(cr.FileInfo.Size)),
				progressbar.OptionSetWriter(os.Stderr),
			)
			cr.Bar.Add(blockSize * len(blocksToSkip))

			if useWebsockets {
				for {
					data := <-dataChan
					if data.err != nil {
						return data.err
					}
					cr.Bar.Add(data.bytesRead)

					// write data to websockets
					err = c.WriteMessage(websocket.BinaryMessage, data.b)
					if err != nil {
						err = errors.Wrap(err, "problem writing message")
						return err
					}
					if bytes.Equal(data.b, []byte("magic")) {
						break
					}
				}
			} else {
				_ = <-isConnectedIfUsingTCP
				log.Debug("connected and ready to send on tcp")
				var wg sync.WaitGroup
				wg.Add(len(tcpConnections))
				for i := range tcpConnections {
					defer func(i int) {
						log.Debugf("closing connection %d", i)
						tcpConnections[i].Close()
					}(i)
					go func(i int, wg *sync.WaitGroup, dataChan <-chan DataChan) {
						defer wg.Done()
						for data := range dataChan {
							if data.err != nil {
								log.Error(data.err)
								return
							}
							cr.Bar.Add(data.bytesRead)
							// write data to tcp connection
							_, err = tcpConnections[i].Write(data.b)
							if err != nil {
								err = errors.Wrap(err, "problem writing message")
								log.Error(err)
								return
							}
							if bytes.Equal(data.b, []byte("magic")) {
								log.Debugf("%d got magic", i)
								return
							}
						}
					}(i, &wg, dataChan)
				}
				wg.Wait()
			}

			cr.Bar.Finish()
			log.Debug("send hash to finish file")
			fileHash, err = utils.HashFile(fname)
			if err != nil {
				return err
			}
		case 6:
			// recevied something, maybe the file hash
			transferTime := time.Since(startTransfer)
			if !bytes.HasPrefix(message, []byte("hash:")) {
				log.Debugf("%s", message)
				continue
			}
			c.WriteMessage(websocket.BinaryMessage, fileHash)
			message = bytes.TrimPrefix(message, []byte("hash:"))
			log.Debugf("[%d] determing whether it went ok", step)
			if bytes.Equal(message, fileHash) {
				log.Debug("file transfered successfully")
				transferRate := float64(cr.FileInfo.Size) / 1000000.0 / transferTime.Seconds()
				transferType := "MB/s"
				if transferRate < 1 {
					transferRate = float64(cr.FileInfo.Size) / 1000.0 / transferTime.Seconds()
					transferType = "kB/s"
				}
				fmt.Fprintf(os.Stderr, "\nTransfer complete (%2.1f %s)", transferRate, transferType)
				cr.StateString = fmt.Sprintf("Transfer complete (%2.1f %s)", transferRate, transferType)
				return nil
			} else {
				fmt.Fprintf(os.Stderr, "\nTransfer corrupted")
				return errors.New("file not transfered succesfully")
			}
		default:
			return fmt.Errorf("unknown step")
		}
		step++
	}
}

func connectToTCPServer(room string, address string) (com comm.Comm, message string, err error) {
	connection, err := net.DialTimeout("tcp", address, 3*time.Hour)
	if err != nil {
		return
	}
	connection.SetReadDeadline(time.Now().Add(3 * time.Hour))
	connection.SetDeadline(time.Now().Add(3 * time.Hour))
	connection.SetWriteDeadline(time.Now().Add(3 * time.Hour))

	com = comm.New(connection)
	ok, err := com.Receive()
	if err != nil {
		return
	}
	log.Debugf("server says: %s", ok)

	err = com.Send(room)
	if err != nil {
		return
	}
	message, err = com.Receive()
	log.Debugf("server says: %s", message)
	return
}
