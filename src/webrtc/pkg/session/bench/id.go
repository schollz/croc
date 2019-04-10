package bench

const (
	// Used as upload channel for master (and download channel for non-master)
	// 43981 -> 0xABCD
	dataChannel1ID = uint16(43981)
	// Used as download channel for master (and upload channel for non-master)
	// 61185 -> 0xef01
	dataChannel2ID = uint16(61185)
)

func (s *Session) uploadChannelID() uint16 {
	if s.master {
		return dataChannel1ID
	}
	return dataChannel2ID
}

func (s *Session) downloadChannelID() uint16 {
	if s.master {
		return dataChannel2ID
	}
	return dataChannel1ID
}
