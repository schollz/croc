//go:build linux

package transferperf

import (
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func platformProfile(profile *SystemProfile) {
	profile.MemoryBytes = readMemAvailable()
	profile.MemoryLimit = readCgroupMemoryLimit()
	if rlimit := readNoFileLimit(); rlimit > 0 {
		profile.RLimitFiles = rlimit
	}
	if profile.MemoryLimit > 0 || readCgroupCPUQuota() > 0 {
		profile.Containerized = true
	}
}

func readMemAvailable() int64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "MemAvailable:" {
			kb, _ := strconv.ParseInt(fields[1], 10, 64)
			return kb * 1024
		}
	}
	return 0
}

func readCgroupMemoryLimit() int64 {
	for _, path := range []string{"/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory/memory.limit_in_bytes"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		value := strings.TrimSpace(string(data))
		if value == "max" || value == "" {
			return 0
		}
		limit, err := strconv.ParseInt(value, 10, 64)
		if err == nil && limit > 0 && limit < 1<<60 {
			return limit
		}
	}
	return 0
}

func readCgroupCPUQuota() float64 {
	data, err := os.ReadFile("/sys/fs/cgroup/cpu.max")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 || fields[0] == "max" {
		return 0
	}
	quota, err1 := strconv.ParseFloat(fields[0], 64)
	period, err2 := strconv.ParseFloat(fields[1], 64)
	if err1 != nil || err2 != nil || period <= 0 {
		return 0
	}
	return quota / period
}

func readNoFileLimit() uint64 {
	var limit unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &limit); err != nil {
		return 0
	}
	return limit.Cur
}
