package croc

import (
	"os"
	"strconv"
)

func (c *Croc) cleanup() {
	// erase all the croc files and their possible numbers
	for i := 0; i < 16; i++ {
		fname := c.crocFile + "." + strconv.Itoa(i)
		os.Remove(fname)
	}
	for i := 0; i < 16; i++ {
		fname := c.crocFileEncrypted + "." + strconv.Itoa(i)
		os.Remove(fname)
	}
	os.Remove(c.crocFile)
	os.Remove(c.crocFileEncrypted)
	c.cs.Lock()
	if c.cs.channel.fileMetaData.DeleteAfterSending {
		os.Remove(c.cs.channel.fileMetaData.Name)
	}
	defer c.cs.Unlock()
}
