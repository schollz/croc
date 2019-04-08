package session

import "io"

// SDPProvider returns the SDP input
func (s *Session) SDPProvider() io.Reader {
	return s.sdpInput
}
