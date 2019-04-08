package sender

import "io"

// SDPProvider returns the underlying SDPProvider
func (s *Session) SDPProvider() io.Reader {
	return s.sess.SDPProvider()
}
