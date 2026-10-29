package croc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	log "github.com/schollz/croc/v11/src/logger"
	"github.com/schollz/croc/v11/src/message"
)

type filePrepared struct {
	Index        int    `json:"i"`
	Hash         []byte `json:"h"`
	IsCompressed bool   `json:"c"`
}

type filePreparationScratch struct {
	compressionSample []byte
	compressionOutput []byte
}

type preparationFailure struct{ err error }

func (c *Client) recordPreparationFailure(err error) {
	c.preparationErr.Store(preparationFailure{err: err})
	c.stop.Cancel()
}

func sourceFilePath(fileInfo FileInfo) string {
	return filepath.Clean(fileInfo.FolderSource + string(os.PathSeparator) + fileInfo.Name)
}

func sourceInfoMatches(expected FileInfo, before, after os.FileInfo) bool {
	if before == nil || after == nil {
		return false
	}
	return expected.Size == before.Size() && expected.ModTime.Equal(before.ModTime()) &&
		expected.Mode == before.Mode() && before.Size() == after.Size() &&
		before.ModTime().Equal(after.ModTime()) && before.Mode() == after.Mode() &&
		os.SameFile(before, after)
}

func (c *Client) prepareFile(index int, algorithm string, scratch *filePreparationScratch) error {
	if index < 0 || index >= len(c.FilesToTransfer) {
		return fmt.Errorf("invalid file preparation index %d", index)
	}
	fileInfo := c.FilesToTransfer[index]
	fullPath := sourceFilePath(fileInfo)
	before, err := os.Lstat(fullPath)
	if err != nil {
		return err
	}

	if !c.Options.NoCompress && fileInfo.Mode.IsRegular() && fileInfo.Size > 0 {
		if scratch.compressionSample == nil {
			scratch.compressionSample = make([]byte, compressionSampleSize)
		}
		c.FilesToTransfer[index].IsCompressed, scratch.compressionOutput = shouldCompressFile(
			fullPath,
			scratch.compressionSample,
			scratch.compressionOutput,
		)
	}
	hash, err := c.stop.hash(fullPath, algorithm, fileInfo.Size > 1e7)
	if err != nil {
		return err
	}
	after, err := os.Lstat(fullPath)
	if err != nil {
		return err
	}
	if !sourceInfoMatches(fileInfo, before, after) {
		return fmt.Errorf("source changed while preparing %s", fullPath)
	}
	c.FilesToTransfer[index].Hash = hash
	c.FilesToTransfer[index].Prepared = true
	c.sourceSnapshots[index] = after
	log.Debugf("hashed %s to %x using %s", fullPath, hash, algorithm)
	return nil
}

func (c *Client) prepareAllFiles(algorithm string, force bool) error {
	var scratch filePreparationScratch
	for i := range c.FilesToTransfer {
		if !force && c.FilesToTransfer[i].Prepared {
			continue
		}
		if err := c.prepareFile(i, algorithm, &scratch); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) finalizeHashNegotiation() error {
	requested := c.Options.HashAlgorithm
	if requested == "" {
		requested = defaultHashAlgorithm
	}
	if c.preparedHashAlgorithm == progressiveHashAlgorithm && c.peerProgressiveHash {
		c.Options.HashAlgorithm = progressiveHashAlgorithm
		return nil
	}
	if c.preparedHashAlgorithm == progressiveHashAlgorithm {
		// Peers without progressive hash support, including v11.3 and current
		// browser receivers, only accept the eager xxhash wire format.
		c.Options.HashAlgorithm = defaultHashAlgorithm
		c.preparedHashAlgorithm = defaultHashAlgorithm
		return c.prepareAllFiles(defaultHashAlgorithm, true)
	}
	c.Options.HashAlgorithm = requested
	return c.prepareAllFiles(requested, false)
}

func (c *Client) startRemainingFilePreparation() {
	c.remainingPreparationOnce.Do(func() {
		go func() {
			ctx := c.clientContext()
			workers := min(4, max(1, runtime.NumCPU()/2))
			jobs := make(chan int)
			type prepResult struct {
				index int
				err   error
			}
			results := make(chan prepResult, workers)
			var wg sync.WaitGroup
			for range workers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					var scratch filePreparationScratch
					for index := range jobs {
						if ctx.Err() != nil {
							return
						}
						err := c.prepareFile(index, c.preparedHashAlgorithm, &scratch)
						select {
						case results <- prepResult{index: index, err: err}:
						case <-ctx.Done():
							return
						}
					}
				}()
			}
			go func() {
				defer close(jobs)
				for i := range c.FilesToTransfer {
					if c.FilesToTransfer[i].Prepared {
						continue
					}
					select {
					case jobs <- i:
					case <-ctx.Done():
						return
					}
				}
			}()
			go func() {
				wg.Wait()
				close(results)
			}()

			useBatch := c.peerSmallFiles && c.peerParallelData
			batch := make([]filePrepared, 0, 64)
			flush := func() error {
				if len(batch) == 0 {
					return nil
				}
				if useBatch {
					body, err := json.Marshal(filePreparedBatch{Files: batch})
					if err != nil {
						return err
					}
					if err := c.sendParallelMessage(message.Message{Type: message.TypeFilePreparedBatch, Bytes: body}); err != nil {
						return err
					}
					batch = batch[:0]
					return nil
				}
				for _, prepared := range batch {
					body, err := json.Marshal(prepared)
					if err != nil {
						return err
					}
					if err := c.sendParallelMessage(message.Message{Type: message.TypeFilePrepared, Bytes: body}); err != nil {
						return err
					}
				}
				batch = batch[:0]
				return nil
			}
			for result := range results {
				if result.err != nil {
					_ = c.sendParallelMessage(message.Message{Type: message.TypeError, Message: "file preparation failed"})
					c.recordPreparationFailure(result.err)
					return
				}
				batch = append(batch, filePrepared{
					Index:        result.index,
					Hash:         c.FilesToTransfer[result.index].Hash,
					IsCompressed: c.FilesToTransfer[result.index].IsCompressed,
				})
				if len(batch) >= 64 {
					if err := flush(); err != nil {
						c.recordPreparationFailure(err)
						return
					}
				}
			}
			if err := flush(); err != nil {
				c.recordPreparationFailure(err)
			}
		}()
	})
}

func (c *Client) validateSourceUnchanged(index int) error {
	if index < 0 || index >= len(c.FilesToTransfer) || index >= len(c.sourceSnapshots) {
		return fmt.Errorf("invalid source index %d", index)
	}
	expected := c.sourceSnapshots[index]
	current, err := os.Lstat(sourceFilePath(c.FilesToTransfer[index]))
	if err != nil {
		return err
	}
	if expected == nil || !sourceInfoMatches(c.FilesToTransfer[index], expected, current) {
		return fmt.Errorf("source changed before transfer: %s", sourceFilePath(c.FilesToTransfer[index]))
	}
	return nil
}
