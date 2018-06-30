package croc

import (
	"testing"

	log "github.com/cihub/seelog"
	"github.com/stretchr/testify/assert"
)

func TestZip(t *testing.T) {
	defer log.Flush()
	writtenFilename, err := zipFile("../testing_data", false)
	assert.Nil(t, err)
	// defer os.Remove(writtenFilename)

	err = unzipFile(writtenFilename, ".")
	assert.Nil(t, err)
	assert.True(t, exists("testing_data"))
	// os.RemoveAll("testing_data")
}
