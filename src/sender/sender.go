package sender

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
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
	"github.com/schollz/progressbar/v2"
	"github.com/schollz/spinner"
	"github.com/tscholl2/siec"
)

var DebugLevel string

// Send is the async call to send data
func Send(forceSend int, serverAddress string, tcpPorts []string, isLocal bool, done chan struct{}, c *websocket.Conn, fname string, codephrase string, useCompression bool, useEncryption bool) {
	logger.SetLogLevel(DebugLevel)
	log.Debugf("sending %s", fname)
	err := send(forceSend, serverAddress, tcpPorts, isLocal, c, fname, codephrase, useCompression, useEncryption)
	if err != nil {
		if !strings.HasPrefix(err.Error(), "websocket: close 100") {
			fmt.Fprintf(os.Stderr, "\n"+err.Error())
		}
	}

	done <- struct{}{}
}

func send(forceSend int, serverAddress string, tcpPorts []string, isLocal bool, c *websocket.Conn, fname string, codephrase string, useCompression bool, useEncryption bool) (err error) {
	var f *os.File
	defer f.Close() // ignore the error if it wasn't opened :(
	var fstats models.FileStats
	var fileHash []byte
	var otherIP string
	var startTransfer time.Time
	var tcpConnections []comm.Comm

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

	// pick an elliptic curve
	curve := siec.SIEC255()
	// both parties should have a weak key
	pw := []byte(codephrase)
	// initialize sender P ("0" indicates sender)
	P, err := pake.Init(pw, 0, curve, 1*time.Millisecond)
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
			// send my IP address
			c.WriteMessage(websocket.BinaryMessage, []byte(ip))
		case 1:
			// first receive the IP address from the sender
			otherIP = string(message)
			log.Debugf("recipient IP: %s", otherIP)

			go func() {
				// recipient might want file! start gathering information about file
				fstat, err := os.Stat(fname)
				if err != nil {
					fileReady <- err
					return
				}
				fstats = models.FileStats{
					Name:         filename,
					Size:         fstat.Size(),
					ModTime:      fstat.ModTime(),
					IsDir:        fstat.IsDir(),
					SentName:     fstat.Name(),
					IsCompressed: useCompression,
					IsEncrypted:  useEncryption,
				}
				if fstats.IsDir {
					// zip the directory
					fstats.SentName, err = zipper.ZipFile(fname, true)
					// remove the file when leaving
					defer os.Remove(fstats.SentName)
					fname = fstats.SentName

					fstat, err := os.Stat(fname)
					if err != nil {
						fileReady <- err
						return
					}
					// get new size
					fstats.Size = fstat.Size()
				}

				// open the file
				f, err = os.Open(fname)
				if err != nil {
					fileReady <- err
					return
				}
				fileReady <- nil

				// start streaming encryption/compression
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
							// do compression
							var compressedBytes []byte
							if useCompression && !fstats.IsDir {
								compressedBytes = compress.Compress(buffer[:bytesread])
							} else {
								compressedBytes = buffer[:bytesread]
							}

							// put number of byte read
							transferBytes, err := json.Marshal(models.BytesAndLocation{Bytes: compressedBytes, Location: currentPostition})

							// do encryption
							enc := crypt.Encrypt(transferBytes, sessionKey, !useEncryption)
							encBytes, err := json.Marshal(enc)
							if err != nil {
								dataChan <- DataChan{
									b:         nil,
									bytesRead: 0,
									err:       err,
								}
								return
							}

							if err != nil {
								dataChan <- DataChan{
									b:         nil,
									bytesRead: 0,
									err:       err,
								}
								return
							}

							select {
							case dataChan <- DataChan{
								b:         encBytes,
								bytesRead: bytesread,
								err:       nil,
							}:
							default:
								log.Debug("blocked")
								// no message sent
								// block
								dataChan <- DataChan{
									b:         encBytes,
									bytesRead: bytesread,
									err:       nil,
								}
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
					dataChan <- DataChan{
						b:         []byte("magic"),
						bytesRead: len([]byte("magic")),
						err:       nil,
					}
					if !useWebsockets {
						for i := 0; i < len(tcpConnections)-1; i++ {
							dataChan <- DataChan{
								b:         []byte("magic"),
								bytesRead: len([]byte("magic")),
								err:       nil,
							}
						}
					}
				}(dataChan)
			}()

			// send pake data
			log.Debugf("[%d] first, P sends u to Q", step)
			c.WriteMessage(websocket.BinaryMessage, P.Bytes())
			// start PAKE spinnner
			spin.Suffix = " performing PAKE..."
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
			spin.Start()
		case 3:
			log.Debugf("[%d] recipient declares readiness for file info", step)
			if !bytes.Equal(message, []byte("ready")) {
				return errors.New("recipient refused file")
			}
			err = <-fileReady // block until file is ready
			if err != nil {
				return err
			}
			fstatsBytes, err := json.Marshal(fstats)
			if err != nil {
				return err
			}

			// encrypt the file meta data
			enc := crypt.Encrypt(fstatsBytes, sessionKey)
			// send the file meta data
			c.WriteMessage(websocket.BinaryMessage, enc.Bytes())
		case 4:
			spin.Stop()

			log.Debugf("[%d] recipient declares readiness for file data", step)
			if !bytes.Equal(message, []byte("ready")) {
				return errors.New("recipient refused file")
			}

			// connect to TCP to receive file
			if !useWebsockets {
				log.Debugf("connecting to server")
				tcpConnections = make([]comm.Comm, len(tcpPorts))
				for i, tcpPort := range tcpPorts {
					log.Debug(tcpPort)
					tcpConnections[i], err = connectToTCPServer(utils.SHA256(fmt.Sprintf("%d%x", i, sessionKey)), serverAddress+":"+tcpPort)
					if err != nil {
						log.Error(err)
						return err
					}
					defer tcpConnections[i].Close()
				}
			}

			fmt.Fprintf(os.Stderr, "\rSending (->%s)...\n", otherIP)
			// send file, compure hash simultaneously
			startTransfer = time.Now()

			bar := progressbar.NewOptions(
				int(fstats.Size),
				progressbar.OptionSetRenderBlankState(true),
				progressbar.OptionSetBytes(int(fstats.Size)),
				progressbar.OptionSetWriter(os.Stderr),
			)

			if useWebsockets {
				for {
					data := <-dataChan
					if data.err != nil {
						return data.err
					}
					bar.Add(data.bytesRead)
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
				for i := range tcpConnections {
					go func(tcpConnection comm.Comm) {
						for {
							data := <-dataChan
							if data.err != nil {
								log.Error(data.err)
								return
							}
							bar.Add(data.bytesRead)
							// write data to tcp connection
							_, err = tcpConnection.Write(data.b)
							if err != nil {
								err = errors.Wrap(err, "problem writing message")
								log.Error(err)
								return
							}
							if bytes.Equal(data.b, []byte("magic")) {
								return
							}

						}
					}(tcpConnections[i])
				}

			}

			bar.Finish()
			log.Debug("send hash to finish file")
			fileHash, err = utils.HashFile(fname)
			if err != nil {
				return err
			}
		case 5:
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
				transferRate := float64(fstats.Size) / 1000000.0 / transferTime.Seconds()
				transferType := "MB/s"
				if transferRate < 1 {
					transferRate = float64(fstats.Size) / 1000.0 / transferTime.Seconds()
					transferType = "kB/s"
				}
				fmt.Fprintf(os.Stderr, "\nTransfer complete (%2.1f %s)", transferRate, transferType)
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

func connectToTCPServer(room string, address string) (com comm.Comm, err error) {
	connection, err := net.Dial("tcp", address)
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
	ok, err = com.Receive()
	log.Debugf("server says: %s", ok)
	if err != nil {
		return
	}
	if ok != "sender" {
		err = errors.New(ok)
	}
	return
}
