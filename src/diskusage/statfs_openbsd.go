//go:build openbsd

package diskusage

import "golang.org/x/sys/unix"

func statFSFreeBytes(stat *unix.Statfs_t) uint64 {
	return uint64(stat.F_bfree) * uint64(stat.F_bsize)
}

func statFSAvailableBytes(stat *unix.Statfs_t) uint64 {
	return uint64(stat.F_bavail) * uint64(stat.F_bsize)
}

func statFSSizeBytes(stat *unix.Statfs_t) uint64 {
	return uint64(stat.F_blocks) * uint64(stat.F_bsize)
}
