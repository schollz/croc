package croc

import (
	"context"
	"net"

	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/progressbar/v3"
)

type transferLifecycle struct {
	ChannelSecured      bool
	FileInfoTransferred bool
	RecipientRequested  bool
	FileTransferred     bool
	CloseChannels       bool
	Successful          bool
}

func (c *Client) lifecycleSnapshot() transferLifecycle {
	c.lifecycleMu.RLock()
	defer c.lifecycleMu.RUnlock()
	return transferLifecycle{
		ChannelSecured:      c.Step1ChannelSecured,
		FileInfoTransferred: c.Step2FileInfoTransferred,
		RecipientRequested:  c.Step3RecipientRequestFile,
		FileTransferred:     c.Step4FileTransferred,
		CloseChannels:       c.Step5CloseChannels,
		Successful:          c.SuccessfulTransfer,
	}
}

func (c *Client) updateLifecycle(update func(*transferLifecycle)) {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	state := transferLifecycle{
		ChannelSecured:      c.Step1ChannelSecured,
		FileInfoTransferred: c.Step2FileInfoTransferred,
		RecipientRequested:  c.Step3RecipientRequestFile,
		FileTransferred:     c.Step4FileTransferred,
		CloseChannels:       c.Step5CloseChannels,
		Successful:          c.SuccessfulTransfer,
	}
	update(&state)
	c.Step1ChannelSecured = state.ChannelSecured
	c.Step2FileInfoTransferred = state.FileInfoTransferred
	c.Step3RecipientRequestFile = state.RecipientRequested
	c.Step4FileTransferred = state.FileTransferred
	c.Step5CloseChannels = state.CloseChannels
	c.SuccessfulTransfer = state.Successful
}

func (c *Client) resetLifecycle() {
	c.updateLifecycle(func(state *transferLifecycle) {
		*state = transferLifecycle{}
	})
}

func (c *Client) waitForFilesReady(ctx context.Context) error {
	select {
	case <-c.filesReady:
		return c.filesReadyErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) finishFilePreparation(err error) {
	c.filesReadyOnce.Do(func() {
		c.filesReadyErr = err
		if err == nil {
			c.markStartup("file-metadata-ready")
		}
		close(c.filesReady)
	})
}

func (c *Client) connection(index int) *comm.Comm {
	c.connectionsMu.RLock()
	defer c.connectionsMu.RUnlock()
	if index < 0 || index >= len(c.conn) {
		return nil
	}
	return c.conn[index]
}

func (c *Client) setConnection(index int, connection *comm.Comm) {
	c.connectionsMu.Lock()
	if index >= len(c.conn) {
		connections := make([]*comm.Comm, index+1)
		copy(connections, c.conn)
		c.conn = connections
	}
	c.conn[index] = connection
	c.connectionsMu.Unlock()
	if index == 0 && connection != nil {
		c.markStartup("relay-control-ready")
	}
}

func (c *Client) connectionsSnapshot() []*comm.Comm {
	c.connectionsMu.RLock()
	defer c.connectionsMu.RUnlock()
	connections := make([]*comm.Comm, len(c.conn))
	copy(connections, c.conn)
	return connections
}

func (c *Client) replaceDataConnections(raw []net.Conn) {
	c.connectionsMu.Lock()
	defer c.connectionsMu.Unlock()
	need := len(raw) + 1
	if len(c.conn) < need {
		connections := make([]*comm.Comm, need)
		copy(connections, c.conn)
		c.conn = connections
	}
	for i := 1; i < len(c.conn); i++ {
		if c.conn[i] != nil {
			c.conn[i].Close()
			c.conn[i] = nil
		}
	}
	for i, connection := range raw {
		c.conn[i+1] = comm.New(connection)
	}
}

func (c *Client) setProgressBar(bar *progressbar.ProgressBar) {
	c.barMu.Lock()
	c.bar = bar
	c.barMu.Unlock()
}

func (c *Client) addProgress(delta int64) {
	c.barMu.RLock()
	bar := c.bar
	c.barMu.RUnlock()
	if bar != nil {
		_ = bar.Add64(delta)
	}
}

func (c *Client) finishProgress() {
	c.barMu.RLock()
	bar := c.bar
	c.barMu.RUnlock()
	if bar != nil {
		_ = bar.Finish()
	}
}
