package croc

import (
	"context"

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
	// existing path, relative to the receive folder (QuestionOverwrite).
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
