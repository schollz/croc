package randomstring

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRandomString(t *testing.T) {
	r, err := GenerateRandomString(20)
	assert.Nil(t, err)
	assert.Equal(t, 20, len(r))
	fmt.Println(r)
}
