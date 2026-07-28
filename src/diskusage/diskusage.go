//go:build !windows && !netbsd
// +build !windows,!netbsd

package diskusage

import (
	"golang.org/x/sys/unix"
)

// DiskUsage contains usage data and provides user-friendly access methods
type DiskUsage struct {
	stat *unix.Statfs_t
}

// NewDiskUsage returns an object holding the disk usage of volumePath
// or nil in case of error (invalid path, etc)
func NewDiskUsage(volumePath string) *DiskUsage {
	stat := unix.Statfs_t{}
	err := unix.Statfs(volumePath, &stat)
	if err != nil {
		return nil
	}
	return &DiskUsage{&stat}
}

// Free returns total free bytes on file system
func (du *DiskUsage) Free() uint64 {
	return statFSFreeBytes(du.stat)
}

// Available returns total available bytes on file system to an unprivileged user
func (du *DiskUsage) Available() uint64 {
	return statFSAvailableBytes(du.stat)
}

// Size returns total size of the file system
func (du *DiskUsage) Size() uint64 {
	return statFSSizeBytes(du.stat)
}

// Used returns total bytes used in file system
func (du *DiskUsage) Used() uint64 {
	return du.Size() - du.Free()
}

// Usage returns percentage of use on the file system
func (du *DiskUsage) Usage() float32 {
	return float32(du.Used()) / float32(du.Size())
}
