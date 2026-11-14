package croc

import (
	"context"
	"path/filepath"

	"github.com/schollz/croc/v11/src/utils"
)

// QuestionKind says which of croc's yes/no questions Options.Prompt is asked.
type QuestionKind int

const (
	// QuestionAccept asks a receiver to accept the offered files.
	QuestionAccept QuestionKind = iota
	// QuestionSend asks a sender running with Ask to send to the peer.
	QuestionSend
	// QuestionOverwrite asks a receiver to replace or resume something that
	// already exists: a file, a non-empty folder or a zip member.
	QuestionOverwrite
)

// Question is one yes/no question croc would otherwise ask on the terminal.
type Question struct {
	Kind QuestionKind
	// Name is the peer's machine ID (QuestionAccept, QuestionSend) or the
	// existing path, relative to OutDir (QuestionOverwrite).
	Name string
	// Files and EmptyFolders are the offer (QuestionAccept only). Files is a
	// shallow copy: the slice is the callback's to keep, FileInfo.Hash is shared.
	Files        []FileInfo
	EmptyFolders int
	// Required reports that croc asks even without a callback. It is false only
	// for an accept question under NoPrompt, which a callback may answer yes
	// without involving a person. Overwrite questions are always Required.
	Required bool
}

// Progress is a snapshot of the file in flight. A chunk that lands just after
// croc moves to the next file may count toward that file.
type Progress struct {
	Index int    // position of the file in the offered list, from 0
	Name  string // FileInfo.Name of the file
	Sent  int64  // bytes of this file moved so far, including bytes kept from an earlier attempt
	Size  int64  // size of this file
}

// answer returns "y" or "n" for q from Options.Prompt, or one line from stdin
// when it is unset, so every call site keeps parsing the answer it parsed before.
func (c *Client) answer(ctx context.Context, q Question) (string, error) {
	if c.Options.Prompt == nil {
		return utils.GetInputContext(ctx, "")
	}
	yes, err := c.Options.Prompt(ctx, q)
	if err != nil || !yes {
		return "n", ctx.Err() // an error other than ctx's is a no
	}
	return "y", nil
}

// reportProgress calls Options.OnProgress under progressMu, so calls neither
// overlap nor reorder across data connections. A bundle passes the bytes of
// file index so far; a single file passes index -1 and the bytes just moved,
// which add to the count kept for the current file.
func (c *Client) reportProgress(index int, sent int64) {
	if c.Options.OnProgress == nil {
		return
	}
	c.progressMu.Lock()
	defer c.progressMu.Unlock()
	if index < 0 {
		index = c.FilesToTransferCurrentNum
		c.progressSent += sent
		sent = c.progressSent
	}
	if index >= len(c.FilesToTransfer) {
		return
	}
	file := c.FilesToTransfer[index]
	c.Options.OnProgress(Progress{Index: index, Name: file.Name, Sent: sent, Size: file.Size})
}

// receivePath is a receive-folder path (slash separated, as in FileInfo) as a
// path the os package can open, for the few calls that do not go through the
// receive root.
func (c *Client) receivePath(name string) string {
	if c.Options.OutDir == "" {
		return name // Join would Clean it, and on Windows change its slashes
	}
	return filepath.Join(c.Options.OutDir, filepath.FromSlash(name))
}
