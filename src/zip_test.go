package croc

import (
	"os"
	"testing"

	log "github.com/cihub/seelog"
	"github.com/stretchr/testify/assert"
)

func TestZip(t *testing.T) {
	defer log.Flush()
	writtenFilename, err := zipFile("../README.md", false)
	assert.Nil(t, err)
	defer os.Remove(writtenFilename)

	err = unzipFile(writtenFilename, ".")
	assert.Nil(t, err)
	assert.True(t, exists("README.md"))
	os.RemoveAll("README.md")
}
