package zipper

import (
	"os"
	"testing"

	log "github.com/cihub/seelog"
	"github.com/schollz/croc/src/utils"
	"github.com/stretchr/testify/assert"
)

func TestZip(t *testing.T) {
	defer log.Flush()
	DebugLevel = "debug"
	writtenFilename, err := ZipFile("../croc", true)
	assert.Nil(t, err)
	defer os.Remove(writtenFilename)
	err = UnzipFile(writtenFilename, ".")
	assert.Nil(t, err)
	assert.True(t, utils.Exists("croc"))

	writtenFilename, err = ZipFile("../../README.md", false)
	assert.Nil(t, err)
	defer os.Remove(writtenFilename)
	err = UnzipFile(writtenFilename, ".")
	assert.Nil(t, err)
	assert.True(t, utils.Exists("README.md"))

}
