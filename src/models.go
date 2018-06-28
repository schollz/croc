package croc

import "crypto/elliptic"

type channelData struct {
	// public
	Name  string `json:"name,omitempty"`
	State map[string][]byte

	// private
	stapled bool
	uuids   [2]string // 0 is sender, 1 is recipient
	curve   elliptic.Curve
}

type response struct {
	// various responses
	Channel string            `json:"channel,omitempty"`
	UUID    string            `json:"uuid,omitempty"`
	State   map[string][]byte `json:"state,omitempty"`

	// constant responses
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type payloadOpen struct {
	Channel string `json:"channel"`
	Role    int    `json:"role"`
	Curve   string `json:"curve"`
}

type payloadChannel struct {
	Channel string            `json:"channel" binding:"required"`
	UUID    string            `json:"uuid" binding:"required"`
	State   map[string][]byte `json:"state"`
	Close   bool              `json:"close"`
}

func newChannelData(name string) (cd *channelData) {
	cd = new(channelData)
	cd.Name = name
	cd.State = make(map[string][]byte)
	cd.State["x"] = []byte{}
	cd.State["curve"] = []byte{}
	cd.State["y"] = []byte{}
	cd.State["hh_k"] = []byte{}
	cd.State["sender_ready"] = []byte{0}
	cd.State["recipient_ready"] = []byte{0}
	cd.State["is_open"] = []byte{0}
	return
}
