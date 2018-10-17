package relay

import (
	"sync"

	log "github.com/cihub/seelog"
)

type message struct {
	msg          messageChannel
	room         string
	remoteOrigin string
}

type subscription struct {
	conn *connection
	room string
}

// hub maintains the set of active connections and broadcasts messages to the
// connections.
type hub struct {
	// Registered connections.
	rooms roomMap

	// Inbound messages from the connections.
	broadcast chan message

	// Register requests from the connections.
	register chan subscription

	// Unregister requests from connections.
	unregister chan subscription
}

type roomMap struct {
	rooms map[string]map[*connection]bool
	sync.Mutex
}

var h = hub{
	broadcast:  make(chan message),
	register:   make(chan subscription),
	unregister: make(chan subscription),
	rooms:      roomMap{rooms: make(map[string]map[*connection]bool)},
}

func (h *hub) run() {
	for {
		if stop {
			log.Debug("stopping hub")
			return
		}
		select {
		case s := <-h.register:
			log.Debugf("adding connection to %s", s.room)
			h.rooms.Lock()
			connections := h.rooms.rooms[s.room]
			if connections == nil {
				connections = make(map[*connection]bool)
				h.rooms.rooms[s.room] = connections
			}
			h.rooms.rooms[s.room][s.conn] = true
			if len(h.rooms.rooms) > 2 {
				// if more than three, close all of them
				for connection := range h.rooms.rooms[s.room] {
					close(connection.send)
				}
				log.Debugf("deleting room %s", s.room)
				delete(h.rooms.rooms, s.room)
			}
			h.rooms.Unlock()
		case s := <-h.unregister:
			// if one leaves, close all of them
			h.rooms.Lock()
			if _, ok := h.rooms.rooms[s.room]; ok {
				for connection := range h.rooms.rooms[s.room] {
					close(connection.send)
				}
				log.Debugf("deleting room %s", s.room)
				delete(h.rooms.rooms, s.room)
			}
			h.rooms.Unlock()
		case m := <-h.broadcast:
			h.rooms.Lock()
			connections := h.rooms.rooms[m.room]
			for c := range connections {
				if c.ws.RemoteAddr().String() == m.remoteOrigin {
					continue
				}
				select {
				case c.send <- m.msg:
				default:
					close(c.send)
					delete(connections, c)
					if len(connections) == 0 {
						log.Debugf("deleting room %s", m.room)
						delete(h.rooms.rooms, m.room)
					}
				}
			}
			h.rooms.Unlock()
		}
	}
}
