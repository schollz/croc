package croc

import (
	"os"
	"strconv"
)

func (c *Croc) cleanup() {
	// erase all the croc files and their possible numbers
	for i := 0; i < 100; i++ {
		fname := c.crocFile + "." + strconv.Itoa(i)
		if !exists(fname) {
			break
		}
		os.Remove(fname)
	}
	for i := 0; i < 100; i++ {
		fname := c.crocFileEncrypted + "." + strconv.Itoa(i)
		if !exists(fname) {
			break
		}
		os.Remove(fname)
	}
	os.Remove(c.crocFile)
	os.Remove(c.crocFileEncrypted)
}
