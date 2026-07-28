//go:build !windows && !netbsd && !openbsd

package diskusage

import "golang.org/x/sys/unix"

func statFSFreeBytes(stat *unix.Statfs_t) uint64 {
	return uint64(stat.Bfree) * uint64(stat.Bsize)
}

func statFSAvailableBytes(stat *unix.Statfs_t) uint64 {
	return uint64(stat.Bavail) * uint64(stat.Bsize)
}

func statFSSizeBytes(stat *unix.Statfs_t) uint64 {
	return uint64(stat.Blocks) * uint64(stat.Bsize)
}
