// ctx.go
package utils

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/minio/highwayhash"
	"github.com/schollz/progressbar/v3"
)

// ctxFile wraps os.File with context cancellation support.
type ctxFile struct {
	ctx context.Context
	f   *os.File
}

// NewCtxFile creates a new context-aware file wrapper.
func NewCtxFile(ctx context.Context, f *os.File) *ctxFile {
	return &ctxFile{ctx: ctx, f: f}
}

// Read implements io.Reader interface with context cancellation.
func (c *ctxFile) Read(p []byte) (n int, err error) {
	select {
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
	default:
		n, err = c.f.Read(p)
		if c.ctx.Err() != nil {
			return 0, c.ctx.Err()
		}
		return n, err
	}
}

// ReadAt implements io.ReaderAt interface with context cancellation.
func (c *ctxFile) ReadAt(p []byte, off int64) (n int, err error) {
	select {
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
	default:
		n, err = c.f.ReadAt(p, off)
		if c.ctx.Err() != nil {
			return 0, c.ctx.Err()
		}
		return n, err
	}
}

// Seek implements io.Seeker interface with context cancellation.
func (c *ctxFile) Seek(offset int64, whence int) (n int64, err error) {
	select {
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
	default:
		n, err = c.f.Seek(offset, whence)
		if c.ctx.Err() != nil {
			return 0, c.ctx.Err()
		}
		return n, err
	}
}

// HashFileCtx returns the hash of a file with context cancellation support.
func HashFileCtx(ctx context.Context, fname string, algorithm string, showProgress ...bool) ([]byte, error) {
	// Quick context check before starting
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	fstats, err := os.Lstat(fname)
	if err != nil {
		return nil, err
	}

	// Handle symlinks - quick operation, no context needed
	if fstats.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(fname)
		if err != nil {
			return nil, err
		}
		return []byte(SHA256(target)), nil
	}

	f, err := os.Open(fname)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Get file info for size (now file is opened, following symlinks if any)
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}

	// Wrap the file with context support
	cf := NewCtxFile(ctx, f)
	sr := io.NewSectionReader(cf, 0, fi.Size())

	// Parse showProgress parameter
	doShowProgress := false
	if len(showProgress) > 0 {
		doShowProgress = showProgress[0]
	}

	// Create progress bar based on algorithm
	var bar *progressbar.ProgressBar
	if doShowProgress {
		fnameShort := path.Base(fname)
		if len(fnameShort) > 20 {
			fnameShort = fnameShort[:20] + "..."
		}

		if algorithm == "imohash" {
			// Spinner for imohash (indeterminate progress, max = -1)
			bar = progressbar.NewOptions64(-1,
				progressbar.OptionSetWriter(os.Stderr),
				progressbar.OptionShowBytes(false),
				progressbar.OptionSetDescription(fmt.Sprintf("Sampling %s", fnameShort)),
				progressbar.OptionClearOnFinish(),
				progressbar.OptionFullWidth(),
				progressbar.OptionShowElapsedTimeOnFinish(),
				progressbar.OptionSpinnerType(14),
				progressbar.OptionSetSpinnerChangeInterval(100*time.Millisecond),
			)
		} else {
			// Regular progress bar for other algorithms
			bar = progressbar.NewOptions64(fi.Size(),
				progressbar.OptionSetWriter(os.Stderr),
				progressbar.OptionShowBytes(true),
				progressbar.OptionSetDescription(fmt.Sprintf("Hashing %s", fnameShort)),
				progressbar.OptionClearOnFinish(),
				progressbar.OptionFullWidth(),
			)
		}
	}

	// Dispatch to appropriate hash function
	switch algorithm {
	case "imohash":
		return IMOHashReader(sr, bar)
	case "md5":
		return MD5HashReader(sr, bar)
	case "xxhash":
		return XXHashReader(sr, bar)
	case "highway":
		return HighwayHashReader(sr, bar)
	default:
		return nil, fmt.Errorf("unsupported algorithm: %s", algorithm)
	}
}

// IMOHashReader returns imohash for a SectionReader.
// Uses spinner progress bar for indeterminate progress.
func IMOHashReader(sr *io.SectionReader, bar *progressbar.ProgressBar) ([]byte, error) {
	// Start spinner if provided
	if bar != nil {
		// Add(0) triggers initial render for spinner
		bar.Add(0)
	}

	b, err := imopartial.SumSectionReader(sr)
	if err != nil {
		// If there's an error, finish the bar to clean up display
		if bar != nil {
			bar.Exit()
		}
		return nil, err
	}

	// Finish the progress bar
	if bar != nil {
		bar.Finish()
	}

	return b[:], nil
}

// IMOHashReaderFull returns full imohash (no sampling) for a SectionReader.
func IMOHashReaderFull(sr *io.SectionReader, bar *progressbar.ProgressBar) ([]byte, error) {
	// For full imohash (which reads entire file), use regular progress bar logic
	if bar != nil {
		bar.Add(0) // Start the spinner
	}

	b, err := imofull.SumSectionReader(sr)
	if err != nil {
		if bar != nil {
			bar.Exit()
		}
		return nil, err
	}

	if bar != nil {
		bar.Finish()
	}

	return b[:], nil
}

// MD5HashReader returns MD5 hash for a SectionReader.
func MD5HashReader(sr *io.SectionReader, bar *progressbar.ProgressBar) ([]byte, error) {
	// Reset to beginning
	if _, err := sr.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	h := md5.New()
	if bar != nil {
		// Copy with progress tracking (like original code)
		if _, err := io.Copy(io.MultiWriter(h, bar), sr); err != nil {
			return nil, err
		}
	} else {
		if _, err := io.Copy(h, sr); err != nil {
			return nil, err
		}
	}
	return h.Sum(nil), nil
}

// XXHashReader returns xxhash for a SectionReader.
func XXHashReader(sr *io.SectionReader, bar *progressbar.ProgressBar) ([]byte, error) {
	if _, err := sr.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	h := xxhash.New()
	if bar != nil {
		if _, err := io.Copy(io.MultiWriter(h, bar), sr); err != nil {
			return nil, err
		}
	} else {
		if _, err := io.Copy(h, sr); err != nil {
			return nil, err
		}
	}
	return h.Sum(nil), nil
}

// HighwayHashReader returns highwayhash for a SectionReader.
func HighwayHashReader(sr *io.SectionReader, bar *progressbar.ProgressBar) ([]byte, error) {
	if _, err := sr.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	key, err := hex.DecodeString("1553c5383fb0b86578c3310da665b4f6e0521acf22eb58a99532ffed02a6b115")
	if err != nil {
		return nil, err
	}

	h, err := highwayhash.New(key)
	if err != nil {
		return nil, fmt.Errorf("could not create highwayhash: %w", err)
	}

	if bar != nil {
		if _, err := io.Copy(io.MultiWriter(h, bar), sr); err != nil {
			return nil, err
		}
	} else {
		if _, err := io.Copy(h, sr); err != nil {
			return nil, err
		}
	}
	return h.Sum(nil), nil
}

// Helper function to update existing HashFile to use HashFileCtx
// func HashFile(fname string, algorithm string, showProgress ...bool) ([]byte, error) {
// 	return HashFileCtx(context.Background(), fname, algorithm, showProgress...)
// }
