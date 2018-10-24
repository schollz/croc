package zipper

import (
	"os"
	"path"
	"testing"

	log "github.com/cihub/seelog"
	"github.com/schollz/croc/src/utils"
	"github.com/stretchr/testify/assert"
)

func TestZip(t *testing.T) {
	defer log.Flush()
	DebugLevel = "debug"
	writtenFilename1, err := ZipFile("../croc", true)
	assert.Nil(t, err)
	err = UnzipFile(writtenFilename1, ".")
	assert.Nil(t, err)
	assert.True(t, utils.Exists("croc"))

	writtenFilename2, err := ZipFile("../../README.md", false)
	assert.Nil(t, err)
	err = UnzipFile(writtenFilename2, ".")
	assert.Nil(t, err)
	assert.True(t, utils.Exists("README.md"))

	os.Remove("README.md")
	os.RemoveAll("croc")
	os.Remove(writtenFilename1)
	os.Remove(writtenFilename2)
}

func TestZipFiles(t *testing.T) {
	defer log.Flush()
	DebugLevel = "debug"
	writtenFilename, err := ZipFiles([]string{"../../LICENSE", "../win/Makefile", "../utils"}, true)
	assert.Nil(t, err)
	err = UnzipFile(writtenFilename, "zipfilestest")
	assert.Nil(t, err)
	assert.True(t, utils.Exists("zipfilestest/LICENSE"))
	assert.True(t, utils.Exists("zipfilestest/Makefile"))
	assert.True(t, utils.Exists("zipfilestest/utils/exists.go"))
	os.RemoveAll("zipfilestest")
	err = os.Remove(path.Join(".", writtenFilename))
	assert.Nil(t, err)
}
