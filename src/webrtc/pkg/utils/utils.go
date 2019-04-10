package utils

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"strings"
)

// MustReadStream blocks until input is received from the stream
func MustReadStream(stream io.Reader) (string, error) {
	r := bufio.NewReader(stream)

	var in string
	for {
		var err error
		in, err = r.ReadString('\n')
		if err != io.EOF {
			if err != nil {
				return "", err
			}
		}
		in = strings.TrimSpace(in)
		if len(in) > 0 {
			break
		}
	}

	fmt.Println("")
	return in, nil
}

// StripSDP remove useless elements from an SDP
func StripSDP(originalSDP string) string {
	finalSDP := strings.Replace(originalSDP, "a=group:BUNDLE audio video data", "a=group:BUNDLE data", -1)
	tmp := strings.Split(finalSDP, "m=audio")
	beginningSdp := tmp[0]

	var endSdp string
	if len(tmp) > 1 {
		tmp = strings.Split(tmp[1], "a=end-of-candidates")
		endSdp = strings.Join(tmp[2:], "a=end-of-candidates")
	} else {
		endSdp = strings.Join(tmp[1:], "a=end-of-candidates")
	}

	finalSDP = beginningSdp + endSdp
	finalSDP = strings.Replace(finalSDP, "\r\n\r\n", "\r\n", -1)
	finalSDP = strings.Replace(finalSDP, "\n\n", "\n", -1)
	return finalSDP
}

// Encode encodes the input in base64
// It can optionally zip the input before encoding
func Encode(obj interface{}) (string, error) {
	b, err := json.Marshal(obj)
	if err != nil {
		return "", err
	}
	var gzbuff bytes.Buffer
	gz, err := gzip.NewWriterLevel(&gzbuff, gzip.BestCompression)
	if err != nil {
		return "", err
	}
	if _, err := gz.Write(b); err != nil {
		return "", err
	}
	if err := gz.Flush(); err != nil {
		return "", err
	}
	if err := gz.Close(); err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(gzbuff.Bytes()), nil
}

// Decode decodes the input from base64
// It can optionally unzip the input after decoding
func Decode(in string, obj interface{}) error {
	b, err := base64.StdEncoding.DecodeString(in)
	if err != nil {
		return err
	}

	gz, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer gz.Close()
	s, err := ioutil.ReadAll(gz)
	if err != nil {
		return err
	}

	return json.Unmarshal(s, obj)
}
