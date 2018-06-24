package base65536

import (
	"errors"
)

const noByte = -1

// Marshal returns the base65536 encoded version of data.
func Marshal(data []byte) string {
	var res string
	var b1 int16
	var b2 int16
	for i := 0; i < len(data); i += 2 {
		b1 = int16(data[i])
		if i+1 < len(data) {
			b2 = int16(data[i+1])
		} else {
			b2 = noByte
		}
		res += string(getBlockStart[b2] + int(b1))
	}
	return res
}

// Unmarshal appends to out the bytes decoded from data. It can return an error,
// in case the decoding failed.
func Unmarshal(data []byte, out *[]byte) error {
	done := false
	var b1 int16
	var b2 int16
	var exists bool
	var bytesAppend []byte
	// We are converting data to a string, because runes
	for _, r := range string(data) {
		b1 = int16(r) & ((1 << 8) - 1)
		b2, exists = getB2[int(r)-int(b1)]
		if !exists {
			return errors.New("not a valid base65536 code point: " + string(int(r)))
		}
		bytesAppend = []byte{
			byte(b1),
		}
		if b2 != noByte {
			bytesAppend = append(bytesAppend, byte(b2))
		}
		if len(bytesAppend) == 1 {
			if done {
				return errors.New("base65536 sequence continued after final byte")
			}
			done = true
		}
		*out = append(*out, bytesAppend...)
	}
	return nil
}
