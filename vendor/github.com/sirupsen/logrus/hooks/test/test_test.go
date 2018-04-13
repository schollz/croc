package test

import (
	"sync"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestAllHooks(t *testing.T) {
	assert := assert.New(t)

	logger, hook := NewNullLogger()
	assert.Nil(hook.LastEntry())
	assert.Equal(0, len(hook.Entries))

	logger.Error("Hello error")
	assert.Equal(logrus.ErrorLevel, hook.LastEntry().Level)
	assert.Equal("Hello error", hook.LastEntry().Message)
	assert.Equal(1, len(hook.Entries))

	logger.Warn("Hello warning")
	assert.Equal(logrus.WarnLevel, hook.LastEntry().Level)
	assert.Equal("Hello warning", hook.LastEntry().Message)
	assert.Equal(2, len(hook.Entries))

	hook.Reset()
	assert.Nil(hook.LastEntry())
	assert.Equal(0, len(hook.Entries))

	hook = NewGlobal()

	logrus.Error("Hello error")
	assert.Equal(logrus.ErrorLevel, hook.LastEntry().Level)
	assert.Equal("Hello error", hook.LastEntry().Message)
	assert.Equal(1, len(hook.Entries))
}

func TestLoggingWithHooksRace(t *testing.T) {
	assert := assert.New(t)
	logger, hook := NewNullLogger()

	var wg sync.WaitGroup
	wg.Add(100)

	for i := 0; i < 100; i++ {
		go func() {
			logger.Info("info")
			wg.Done()
		}()
	}

	assert.Equal(logrus.InfoLevel, hook.LastEntry().Level)
	assert.Equal("info", hook.LastEntry().Message)

	wg.Wait()

	entries := hook.AllEntries()
	assert.Equal(100, len(entries))
}
