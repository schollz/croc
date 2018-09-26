package tcp

import (
	"net"
	"sync"
	"time"

	log "github.com/cihub/seelog"
	"github.com/pkg/errors"
	"github.com/schollz/croc/src/comm"
	"github.com/schollz/croc/src/logger"
	"github.com/schollz/croc/src/models"
)

type roomInfo struct {
	receiver *comm.Comm
	opened   time.Time
}

type roomMap struct {
	rooms map[string]roomInfo
	sync.Mutex
}

var rooms roomMap

// Run starts a tcp listener, run async
func Run(debugLevel, port string) {
	logger.SetLogLevel(debugLevel)
	rooms.Lock()
	rooms.rooms = make(map[string]roomInfo)
	rooms.Unlock()
	err := run(port)
	if err != nil {
		log.Error(err)
	}

	// TODO:
	// delete old rooms
}

func run(port string) (err error) {
	log.Debugf("starting TCP server on " + port)
	// rAddr, err := net.ResolveTCPAddr("tcp", "0.0.0.0:"+port)
	// if err != nil {
	// 	panic(err)
	// }
	// server, err := net.ListenTCP("tcp", rAddr)
	// if err != nil {
	// 	return errors.Wrap(err, "Error listening on :"+port)
	// }
	server, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return errors.Wrap(err, "Error listening on :"+port)
	}
	defer server.Close()
	// spawn a new goroutine whenever a client connects
	for {
		// connection, err := server.AcceptTCP()
		connection, err := server.Accept()
		if err != nil {
			return errors.Wrap(err, "problem accepting connection")
		}
		log.Debugf("client %s connected", connection.RemoteAddr().String())
		go func(port string, connection net.Conn) {
			errCommunication := clientCommuncation(port, comm.New(connection))
			if errCommunication != nil {
				log.Warnf("relay-%s: %s", connection.RemoteAddr().String(), errCommunication.Error())
			}
		}(port, connection)
	}
}

func clientCommuncation(port string, c *comm.Comm) (err error) {
	// send ok to tell client they are connected
	err = c.Send("ok")
	if err != nil {
		return
	}

	// wait for client to tell me which room they want
	room, err := c.Receive()
	if err != nil {
		return
	}

	rooms.Lock()
	// first connection is always the receiver
	if _, ok := rooms.rooms[room]; !ok {
		rooms.rooms[room] = roomInfo{
			receiver: c,
			opened:   time.Now(),
		}
		rooms.Unlock()
		// tell the client that they got the room
		err = c.Send("recipient")
		if err != nil {
			return
		}
		return nil
	}
	receiver := rooms.rooms[room].receiver
	rooms.Unlock()

	// second connection is the sender, time to staple connections
	var wg sync.WaitGroup
	wg.Add(1)

	// start piping
	go func(com1, com2 *comm.Comm, wg *sync.WaitGroup) {
		log.Debug("starting pipes")
		pipe(com1.Connection(), com2.Connection())
		wg.Done()
		log.Debug("done piping")
	}(c, receiver, &wg)

	// tell the sender everything is ready
	err = c.Send("sender")
	if err != nil {
		return
	}
	wg.Wait()

	// delete room
	rooms.Lock()
	log.Debugf("deleting room: %s", room)
	delete(rooms.rooms, room)
	rooms.Unlock()
	return nil
}

// chanFromConn creates a channel from a Conn object, and sends everything it
//  Read()s from the socket to the channel.
func chanFromConn(conn net.Conn) chan []byte {
	c := make(chan []byte)
	// reader := bufio.NewReader(conn)

	go func() {
		for {
			b := make([]byte, models.TCP_BUFFER_SIZE)
			n, err := conn.Read(b)
			if n > 0 {
				// c <- b[:n]
				res := make([]byte, n)
				// Copy the buffer so it doesn't get changed while read by the recipient.
				copy(res, b[:n])
				c <- res
			}
			if err != nil {
				c <- nil
				break
			}
		}
	}()

	return c
}

// pipe creates a full-duplex pipe between the two sockets and
// transfers data from one to the other.
func pipe(conn1 net.Conn, conn2 net.Conn) {
	chan1 := chanFromConn(conn1)
	// chan2 := chanFromConn(conn2)
	// writer1 := bufio.NewWriter(conn1)
	// writer2 := bufio.NewWriter(conn2)

	for {
		b1 := <-chan1
		if b1 == nil {
			return
		}
		conn2.Write(b1)
		// writer2.Write(b1)
		// writer2.Flush()

		// case b2 := <-chan2:
		// 	if b2 == nil {
		// 		return
		// 	}
		// 	writer1.Write(b2)
		// 	writer1.Flush()
		// }

	}
}
