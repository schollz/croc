package transferperf

import (
	"context"
	"os"
	"runtime"
	"sync"
	"time"
)

// SystemProfile describes the resources available to this process. Zero values
// mean "unknown" and callers must fall back to conservative defaults.
type SystemProfile struct {
	CPUs          int
	MemoryBytes   int64
	MemoryLimit   int64
	RLimitFiles   uint64
	DiskReadBps   int64
	DiskWriteBps  int64
	Platform      string
	Containerized bool
}

// Probe collects a SystemProfile. Implementations must never block the
// transfer: they should return conservative values on error or timeout.
type Probe interface {
	Probe(ctx context.Context) SystemProfile
}

// DefaultProbe collects process and host resource information. Disk
// benchmarking is optional because it writes temporary data.
type DefaultProbe struct {
	CacheDir string
	SkipDisk bool
}

type diskSample struct {
	readBps  int64
	writeBps int64
}

var (
	diskProbeMu    sync.Mutex
	diskProbeCache = map[string]diskSample{}
)

// Probe returns a resource profile. Failures degrade to zero values rather
// than failing the transfer.
func (p *DefaultProbe) Probe(ctx context.Context) SystemProfile {
	profile := SystemProfile{
		CPUs:     runtime.NumCPU(),
		Platform: runtime.GOOS,
	}
	platformProfile(&profile)
	if !p.SkipDisk && p.CacheDir != "" {
		if sample, ok := cachedDiskSample(p.CacheDir); ok {
			profile.DiskReadBps = sample.readBps
			profile.DiskWriteBps = sample.writeBps
		} else if sample, ok := benchmarkDisk(ctx, p.CacheDir); ok {
			storeDiskSample(p.CacheDir, sample)
			profile.DiskReadBps = sample.readBps
			profile.DiskWriteBps = sample.writeBps
		}
	}
	return profile
}

func cachedDiskSample(key string) (diskSample, bool) {
	diskProbeMu.Lock()
	defer diskProbeMu.Unlock()
	sample, ok := diskProbeCache[key]
	return sample, ok
}

func storeDiskSample(key string, sample diskSample) {
	diskProbeMu.Lock()
	defer diskProbeMu.Unlock()
	diskProbeCache[key] = sample
}

func benchmarkDisk(ctx context.Context, dir string) (diskSample, bool) {
	const size = 2 << 20
	if err := ctx.Err(); err != nil {
		return diskSample{}, false
	}
	file, err := os.CreateTemp(dir, ".croc-probe-")
	if err != nil {
		return diskSample{}, false
	}
	name := file.Name()
	defer os.Remove(name)
	buf := make([]byte, 64<<10)
	start := time.Now()
	written := 0
	for written < size {
		if ctx.Err() != nil {
			file.Close()
			return diskSample{}, false
		}
		n, writeErr := file.Write(buf)
		if writeErr != nil {
			file.Close()
			return diskSample{}, false
		}
		written += n
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return diskSample{}, false
	}
	writeElapsed := time.Since(start)
	if _, err := file.Seek(0, 0); err != nil {
		file.Close()
		return diskSample{}, false
	}
	start = time.Now()
	read := 0
	for read < size {
		if ctx.Err() != nil {
			file.Close()
			return diskSample{}, false
		}
		n, readErr := file.Read(buf)
		if n == 0 && readErr != nil {
			break
		}
		read += n
	}
	readElapsed := time.Since(start)
	file.Close()
	if writeElapsed <= 0 || readElapsed <= 0 || read != size {
		return diskSample{}, false
	}
	return diskSample{
		writeBps: int64(float64(size) / writeElapsed.Seconds()),
		readBps:  int64(float64(read) / readElapsed.Seconds()),
	}, true
}
