package croc

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"strings"

	log "github.com/schollz/croc/v11/src/logger"
	"github.com/schollz/croc/v11/src/receivefs"
	"github.com/schollz/croc/v11/src/termui"
)

type receiveDecision struct{ requested, fresh, pending bool }

// Only user consent is cached. Disk contents are checked again after reception,
// so verification never depends on invalidating a planning cache.
func (c *Client) preflightReceiveFile(index int) (receiveDecision, error) {
	file := c.FilesToTransfer[index]
	if _, done := c.FilesHasFinished[index]; done || file.declined {
		return receiveDecision{}, nil
	}
	if c.progressiveHashActive() && !file.Prepared {
		return receiveDecision{pending: true}, nil
	}
	root, err := c.receiveFilesystem()
	if err != nil {
		return receiveDecision{}, err
	}
	destination := path.Join(file.FolderRemote, file.Name)
	if err = root.RejectSymlinkPath(path.Dir(destination)); err != nil {
		return receiveDecision{}, err
	}
	stat, err := root.Lstat(destination)
	if err != nil && !os.IsNotExist(err) {
		return receiveDecision{}, err
	}
	exists := err == nil
	retained := false
	if index < len(c.bundles.identities) {
		_, retained = c.bundleCommitted[c.bundles.identities[index]]
	}
	matches := false
	if exists && file.Size > 0 && file.Symlink == "" && stat.Mode().IsRegular() && stat.Size() == file.Size {
		hash, err := receiveFileHash(c.clientContext(), destination, c.Options.HashAlgorithm, !c.Options.SendingText)
		if err != nil {
			return receiveDecision{}, fmt.Errorf("hash existing destination %q: %w", destination, err)
		}
		matches = bytes.Equal(hash, file.Hash)
		if matches && c.progressiveHashActive() {
			known, exact, pending := c.exactHashDecision(index)
			switch {
			case known:
				matches = exact
			case pending:
				return receiveDecision{pending: true}, nil
			default:
				if err := c.beginExactHash(index, destination); err != nil {
					return receiveDecision{}, err
				}
				return receiveDecision{pending: true}, nil
			}
		}
	}
	if exists && retained && file.Size == 0 && file.Symlink == "" && stat.Mode().IsRegular() && stat.Size() == 0 {
		matches = true
	}
	if exists && retained && file.Symlink != "" && stat.Mode()&os.ModeSymlink != 0 {
		target, err := root.Readlink(destination)
		if err != nil {
			return receiveDecision{}, err
		}
		matches = target == file.Symlink
	}
	if matches {
		if c.FilesHasFinished == nil {
			c.FilesHasFinished = make(map[int]struct{})
		}
		c.FilesHasFinished[index] = struct{}{}
		if !retained && !c.receiverWroteFile(index) {
			c.numberOfUnchangedFiles++
		}
		if !file.ModTime.IsZero() && file.Symlink == "" {
			if err := root.Chtimes(destination, file.ModTime, file.ModTime); err != nil {
				log.Warnf("chtimes: %v", err)
			}
		}
		return receiveDecision{}, nil
	}
	textArtifact := c.Options.SendingText && strings.HasPrefix(path.Base(file.Name), "croc-stdin-")
	if exists && !textArtifact && c.Options.Rename && !c.receiverWroteFile(index) {
		name, err := c.unusedReceiveName(root, index)
		if err != nil {
			return receiveDecision{}, err
		}
		output, color := termui.Output(os.Stderr)
		fmt.Fprintf(output, "Receiving %s as %s\n", quotedFilename(file.Name, color), quotedFilename(name, color))
		c.FilesToTransfer[index].Name = name
		return receiveDecision{requested: true, fresh: true}, nil
	}
	if exists && !textArtifact && !c.Options.Overwrite && !c.Options.Rename && !c.receiveChoices[index] {
		accepted, err := askReceiveOverwrite(c.clientContext(), file, file.Size > 0 && file.Symlink == "" && stat.Mode().IsRegular())
		if err != nil {
			return receiveDecision{}, err
		}
		if !accepted {
			c.FilesToTransfer[index].declined = true
			return receiveDecision{}, nil
		}
		if c.receiveChoices == nil {
			c.receiveChoices = make(map[int]bool)
		}
		c.receiveChoices[index] = true
	}
	return receiveDecision{requested: true, fresh: !exists}, nil
}

func (c *Client) unusedReceiveName(root *receivefs.Root, index int) (string, error) {
	file := c.FilesToTransfer[index]
	reserved := make(map[string]bool, len(c.FilesToTransfer))
	for i, other := range c.FilesToTransfer {
		if i != index {
			reserved[receivefs.CollisionKey(path.Join(other.FolderRemote, other.Name))] = true
		}
	}
	ext := path.Ext(file.Name)
	base := strings.TrimSuffix(file.Name, ext)
	for n := 1; ; n++ {
		name := fmt.Sprintf("%s (%d)%s", base, n, ext)
		destination := path.Join(file.FolderRemote, name)
		if reserved[receivefs.CollisionKey(destination)] {
			continue
		}
		if _, err := root.Lstat(destination); os.IsNotExist(err) {
			return name, nil
		} else if err != nil {
			return "", err
		}
	}
}
