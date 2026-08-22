package receivefs

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"time"
)

// Root performs receive operations relative to one opened destination root.
// os.Root keeps operations contained even when path components are replaced
// concurrently.
type Root struct {
	root *os.Root
	name string
}

// OpenRoot opens an existing destination directory.
func OpenRoot(name string) (*Root, error) {
	absolute, err := filepath.Abs(name)
	if err != nil {
		return nil, fmt.Errorf("resolve receive root: %w", err)
	}
	opened, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, fmt.Errorf("open receive root: %w", err)
	}
	return &Root{root: opened, name: absolute}, nil
}

func (r *Root) Close() error { return r.root.Close() }
func (r *Root) Name() string { return r.name }

func native(name string, allowRoot bool) (string, error) {
	clean, err := Normalize(name, allowRoot)
	if err != nil {
		return "", err
	}
	return filepath.FromSlash(clean), nil
}

func (r *Root) Open(name string) (*os.File, error) {
	clean, err := native(name, false)
	if err != nil {
		return nil, err
	}
	if err = r.RejectSymlinkPath(name); err != nil {
		return nil, err
	}
	return r.root.Open(clean)
}

// OpenFile opens a validated path while preserving os.OpenFile flag behavior.
func (r *Root) OpenFile(name string, flag int, perm fs.FileMode) (*os.File, error) {
	clean, err := native(name, false)
	if err != nil {
		return nil, err
	}
	if err = r.RejectSymlinkPath(name); err != nil {
		return nil, err
	}
	return r.root.OpenFile(clean, flag, perm.Perm())
}

func (r *Root) Lstat(name string) (fs.FileInfo, error) {
	clean, err := native(name, true)
	if err != nil {
		return nil, err
	}
	return r.root.Lstat(clean)
}

func (r *Root) Stat(name string) (fs.FileInfo, error) {
	clean, err := native(name, true)
	if err != nil {
		return nil, err
	}
	return r.root.Stat(clean)
}

func (r *Root) MkdirAll(name string, perm fs.FileMode) error {
	clean, err := native(name, true)
	if err != nil {
		return err
	}
	if clean == "." {
		return nil
	}
	if err = r.RejectSymlinkPath(name); err != nil {
		return err
	}
	return r.root.MkdirAll(clean, perm.Perm())
}

func (r *Root) Remove(name string) error {
	clean, err := native(name, false)
	if err != nil {
		return err
	}
	return r.root.Remove(clean)
}

func (r *Root) RemoveAll(name string) error {
	clean, err := native(name, false)
	if err != nil {
		return err
	}
	return r.root.RemoveAll(clean)
}

func (r *Root) Rename(oldName, newName string) error {
	oldClean, err := native(oldName, false)
	if err != nil {
		return err
	}
	newClean, err := native(newName, false)
	if err != nil {
		return err
	}
	if err = r.RejectSymlinkPath(path.Dir(newName)); err != nil {
		return err
	}
	return r.root.Rename(oldClean, newClean)
}

func (r *Root) Symlink(target, name string) error {
	cleanName, err := native(name, false)
	if err != nil {
		return err
	}
	cleanTarget, err := Normalize(target, false)
	if err != nil {
		return fmt.Errorf("unsafe symlink target: %w", err)
	}
	if err = r.RejectSymlinkPath(path.Dir(name)); err != nil {
		return err
	}
	return r.root.Symlink(filepath.FromSlash(cleanTarget), cleanName)
}

func (r *Root) Chtimes(name string, atime, mtime time.Time) error {
	clean, err := native(name, false)
	if err != nil {
		return err
	}
	return r.root.Chtimes(clean, atime, mtime)
}

// RejectSymlinkPath preserves croc's refusal of existing symlink components.
// The subsequent os.Root operation supplies containment if a component changes
// after this inspection.
func (r *Root) RejectSymlinkPath(name string) error {
	clean, err := Normalize(name, true)
	if err != nil {
		return err
	}
	if clean == "." {
		return nil
	}
	current := ""
	for _, component := range splitPath(clean) {
		current = path.Join(current, component)
		info, statErr := r.root.Lstat(filepath.FromSlash(current))
		if errors.Is(statErr, fs.ErrNotExist) {
			return nil
		}
		if statErr != nil {
			return fmt.Errorf("inspect destination %q: %w", current, statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to open symlink destination path component %q", current)
		}
	}
	return nil
}

func splitPath(name string) []string {
	if name == "." || name == "" {
		return nil
	}
	var parts []string
	for name != "." && name != "/" {
		parts = append([]string{path.Base(name)}, parts...)
		name = path.Dir(name)
	}
	return parts
}

// CreateTemp creates an exclusive random file below dir and returns its
// root-relative slash path.
func (r *Root) CreateTemp(dir, prefix string, perm fs.FileMode) (*os.File, string, error) {
	cleanDir, err := Normalize(dir, true)
	if err != nil {
		return nil, "", err
	}
	if err = r.MkdirAll(cleanDir, 0o700); err != nil {
		return nil, "", err
	}
	for range 100 {
		var random [12]byte
		if _, err = rand.Read(random[:]); err != nil {
			return nil, "", fmt.Errorf("generate temporary filename: %w", err)
		}
		name := prefix + hex.EncodeToString(random[:])
		if cleanDir != "." {
			name = path.Join(cleanDir, name)
		}
		file, openErr := r.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, perm)
		if errors.Is(openErr, fs.ErrExist) {
			continue
		}
		if openErr != nil {
			return nil, "", openErr
		}
		return file, name, nil
	}
	return nil, "", errors.New("could not allocate a unique temporary file")
}

// CreateTempDir creates an exclusive private directory below dir and returns
// its root-relative slash path.
func (r *Root) CreateTempDir(dir, prefix string) (string, error) {
	cleanDir, err := Normalize(dir, true)
	if err != nil {
		return "", err
	}
	if err = r.MkdirAll(cleanDir, 0o700); err != nil {
		return "", err
	}
	for range 100 {
		var random [12]byte
		if _, err = rand.Read(random[:]); err != nil {
			return "", fmt.Errorf("generate temporary directory name: %w", err)
		}
		name := prefix + hex.EncodeToString(random[:])
		if cleanDir != "." {
			name = path.Join(cleanDir, name)
		}
		cleanName, cleanErr := native(name, false)
		if cleanErr != nil {
			return "", cleanErr
		}
		mkdirErr := r.root.Mkdir(cleanName, 0o700)
		if errors.Is(mkdirErr, fs.ErrExist) {
			continue
		}
		if mkdirErr != nil {
			return "", mkdirErr
		}
		return name, nil
	}
	return "", errors.New("could not allocate a unique temporary directory")
}

// WriteFileAtomic writes, syncs, and renames a private file over name.
func (r *Root) WriteFileAtomic(name string, data []byte, perm fs.FileMode) error {
	clean, err := Normalize(name, false)
	if err != nil {
		return err
	}
	dir := path.Dir(clean)
	temp, tempName, err := r.CreateTemp(dir, ".croc-write-", 0o600)
	if err != nil {
		return err
	}
	defer r.Remove(tempName)
	if _, err = temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Chmod(perm.Perm()); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return r.Rename(tempName, clean)
}
