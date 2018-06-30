package pake

import (
	"crypto/elliptic"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPake(t *testing.T) {
	// successful (both have same k)
	// initialize A
	A, err := Init([]byte{1, 2, 3}, 0, elliptic.P256())
	assert.Nil(t, err)
	assert.False(t, A.IsVerified())
	// initialize B
	B, err := Init([]byte{1, 2, 3}, 1, elliptic.P256())
	assert.Nil(t, err)
	assert.False(t, B.IsVerified())
	// send A's stuff to B
	err = B.Update(A.Bytes())
	assert.Nil(t, err)
	assert.False(t, B.IsVerified())
	// send B's stuff to A
	err = A.Update(B.Bytes())
	assert.Nil(t, err) // A validates
	assert.True(t, A.IsVerified())
	// send A's stuff back to B
	err = B.Update(A.Bytes())
	assert.Nil(t, err) // B validates
	assert.True(t, B.IsVerified())

	// failure (both have different k)
	// initialize A
	A, err = Init([]byte{1, 2, 3}, 0, elliptic.P256())
	assert.Nil(t, err)
	assert.False(t, A.IsVerified())
	// initialize B
	B, err = Init([]byte{4, 5, 6}, 1, elliptic.P256())
	assert.Nil(t, err)
	assert.False(t, B.IsVerified())
	// send A's stuff to B
	err = B.Update(A.Bytes())
	assert.Nil(t, err)
	assert.False(t, B.IsVerified())
	// send B's stuff to A
	err = A.Update(B.Bytes())
	assert.NotNil(t, err) // A validates
	assert.False(t, A.IsVerified())
	// send A's stuff back to B
	err = B.Update(A.Bytes())
	assert.NotNil(t, err)
	assert.False(t, B.IsVerified())

}
