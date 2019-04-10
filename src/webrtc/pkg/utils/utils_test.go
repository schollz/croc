package utils

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_ReadStream(t *testing.T) {
	assert := assert.New(t)
	stream := &bytes.Buffer{}

	_, err := stream.WriteString("Hello\n")
	assert.Nil(err)

	str, err := MustReadStream(stream)
	assert.Equal("Hello", str)
	assert.Nil(err)
}

func Test_StripSDP(t *testing.T) {
	assert := assert.New(t)

	tests := []struct {
		sdp      string
		expected string
	}{
		{
			sdp:      "",
			expected: "",
		},
		{
			sdp: `v=0
o=- 297292268 1552262038 IN IP4 0.0.0.0
s=-
t=0 0
a=fingerprint:sha-256 70:E0:B2:DA:F8:04:D6:0C:32:03:DF:CD:A8:70:EC:45:10:FF:66:6F:3D:72:B1:BA:4C:AF:FB:5E:BE:F9:CF:6A
a=group:BUNDLE audio video data
m=audio 9 UDP/TLS/RTP/SAVPF 111 9
c=IN IP4 0.0.0.0
a=setup:actpass
a=mid:audio
a=ice-ufrag:SNxNaqIiaNoDiCNM
a=ice-pwd:dSZlwOEOKEmBfNiXCtpmPTOVJlwUCaFX
a=rtcp-mux
a=rtcp-rsize
a=rtpmap:111 opus/48000/2
a=fmtp:111 minptime=10;useinbandfec=1
a=rtpmap:9 G722/8000
a=recvonly
a=candidate:foundation 1 udp 3776 192.168.100.207 61879 typ host generation 0
a=candidate:foundation 2 udp 3776 192.168.100.207 61879 typ host generation 0
a=end-of-candidates
a=setup:actpass
m=video 9 UDP/TLS/RTP/SAVPF 96 100 98
c=IN IP4 0.0.0.0
a=setup:actpass
a=mid:video
a=ice-ufrag:SNxNaqIiaNoDiCNM
a=ice-pwd:dSZlwOEOKEmBfNiXCtpmPTOVJlwUCaFX
a=rtcp-mux
a=rtcp-rsize
a=rtpmap:96 VP8/90000
a=rtpmap:100 H264/90000
a=fmtp:100 level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42001f
a=rtpmap:98 VP9/90000
a=recvonly
a=candidate:foundation 1 udp 3776 192.168.100.207 61879 typ host generation 0
a=candidate:foundation 2 udp 3776 192.168.100.207 61879 typ host generation 0
a=end-of-candidates
a=setup:actpass
m=application 9 DTLS/SCTP 5000
c=IN IP4 0.0.0.0
a=setup:actpass
a=mid:data
a=sendrecv
a=sctpmap:5000 webrtc-datachannel 1024
a=ice-ufrag:SNxNaqIiaNoDiCNM
a=ice-pwd:dSZlwOEOKEmBfNiXCtpmPTOVJlwUCaFX
a=candidate:foundation 1 udp 3776 192.168.100.207 61879 typ host generation 0
a=candidate:foundation 2 udp 3776 192.168.100.207 61879 typ host generation 0
a=end-of-candidates
a=setup:actpass
`,
			expected: `v=0
o=- 297292268 1552262038 IN IP4 0.0.0.0
s=-
t=0 0
a=fingerprint:sha-256 70:E0:B2:DA:F8:04:D6:0C:32:03:DF:CD:A8:70:EC:45:10:FF:66:6F:3D:72:B1:BA:4C:AF:FB:5E:BE:F9:CF:6A
a=group:BUNDLE data
a=setup:actpass
m=application 9 DTLS/SCTP 5000
c=IN IP4 0.0.0.0
a=setup:actpass
a=mid:data
a=sendrecv
a=sctpmap:5000 webrtc-datachannel 1024
a=ice-ufrag:SNxNaqIiaNoDiCNM
a=ice-pwd:dSZlwOEOKEmBfNiXCtpmPTOVJlwUCaFX
a=candidate:foundation 1 udp 3776 192.168.100.207 61879 typ host generation 0
a=candidate:foundation 2 udp 3776 192.168.100.207 61879 typ host generation 0
a=end-of-candidates
a=setup:actpass
`,
		},
	}

	for _, cur := range tests {
		assert.Equal(cur.expected, StripSDP(cur.sdp))
	}
}

func Test_Encode(t *testing.T) {
	assert := assert.New(t)

	tests := []struct {
		input     interface{}
		shouldErr bool
		expected  string
	}{
		// Invalid object
		{
			input:     make(chan int),
			shouldErr: true,
		},
		// Empty input
		{
			input:     nil,
			shouldErr: false,
			expected:  "H4sIAAAAAAAC/8orzckBAAAA//8BAAD//0/8yyUEAAAA",
		},
		// Not JSON
		{
			input:     "ThisTestIsNotInB64",
			shouldErr: false,
			expected:  "H4sIAAAAAAAC/1IKycgsDkktLvEs9ssv8cxzMjNRAgAAAP//AQAA//8+sWiWFAAAAA==",
		},
		// JSON
		{
			input: struct {
				Name string `json:"name"`
			}{
				Name: "TestJson",
			},
			shouldErr: false,
			expected:  "H4sIAAAAAAAC/6pWykvMTVWyUgpJLS7xKs7PU6oFAAAA//8BAAD//3cqgZQTAAAA",
		},
	}

	for _, cur := range tests {
		res, err := Encode(cur.input)

		if cur.shouldErr {
			assert.NotNil(err)
		} else {
			assert.Nil(err)
			assert.Equal(cur.expected, res)
		}
	}
}

func Test_Decode(t *testing.T) {
	assert := assert.New(t)

	tests := []struct {
		input     string
		shouldErr bool
	}{
		// Empty string
		{
			input:     "",
			shouldErr: true,
		},
		// Not base64
		{
			input:     "ThisTestIsNotInB64",
			shouldErr: true,
		},
		// Not base64 JSON
		{
			input:     "aGVsbG8gd29ybGQ=",
			shouldErr: true,
		},
		// Base64 JSON
		{
			input:     "H4sIAAAAAAAC/+xVTY/bNhD9KwOdK5ukqK8JdFh77XabNPXGXqcJcmFEystGogiKsuMU/e+FLG/qtEWBBRo0h4UgCTPz+OaRegP9FvijVQEGwnQH5YLvgk7aAIN9Qd65d6YtQojzmCRpmmZA45ixhFNO4eYl3Kw4kMnpGqBdEQ4vXxA4xaKotNkpZ502Hrt7EbI4gYjiLMKE4oIhYZgskWXIyfCcLZFdY5ZgxnEeYTRDHmGeYsyQ57icIWdI5ji/wnmClCBLMUowjjBLcXGFEUHOx8Y71/YWZ3cvr18sQPRSt7DXUrUghRcDpCnGbA5316vp5sV6+mqzmq6vtqslUEohH0Bl8fdNiqJTvrcoSq/3asw0WuKJbgx1qcK+cmKH5avl6zflzeGu8z9vd4df/qzbg8TXerH7cfnrQj9/u/a36+2b55ubulosDx9u5eb2p7cj2vnShk3/8SJynf6kHmLbCIuD5Nb23ZRnhJApOx9/48dSo431ulEFJc/6TmnzXhhZqbKgX7Dk8H3K2HSgOCs1l9sshZFaCq+wansjhdetAQq9tBClaQI0ZxOaZBMyoVkOcRQnCfijhfu287BTRrlxCfkXOvbf0p3VUUKApxM63CyfRAxinhJ6outcVX8EJ6R0f2npbOv8Gfk4+V+jnzIybKvwc9tutPFo63+ycZ7AoCPPHmvlE+X/ZuU8ge0qm+bkswsfPE4I/MASflkaHU4I1Gqv6lB0x6ZR3h1DUdftQcmCPrOi/KC8/nQ6zLBppRqSrq10rcJxmZYFZ4TQ6kshGWxX+WW3p3H45sdBWFvrckTmcD1MxHq+WUF8/oiPmYOHf8VQN9Kpcn+OytEgAycc1Hvny3DAlvfCGFUDJYx/jfF5Mtw3Z7jg9z8AAAD//wEAAP//RjpVQj8JAAA=",
			shouldErr: false,
		},
	}

	var obj interface{}
	for _, cur := range tests {
		err := Decode(cur.input, &obj)

		if cur.shouldErr {
			assert.NotNil(err)
		} else {
			assert.Nil(err)
		}
	}
}

func Test_EncodeDecode(t *testing.T) {
	assert := assert.New(t)

	input := struct {
		Name string `json:"name"`
	}{
		Name: "TestJson",
	}

	encoded, err := Encode(input)
	assert.Nil(err)
	assert.Equal("H4sIAAAAAAAC/6pWykvMTVWyUgpJLS7xKs7PU6oFAAAA//8BAAD//3cqgZQTAAAA", encoded)

	var obj struct {
		Name string `json:"name"`
	}
	err = Decode(encoded, &obj)
	assert.Nil(err)
	assert.Equal(input, obj)
}
