//go:build !linux

package transferperf

import "runtime"

func platformProfile(profile *SystemProfile) {
	profile.MemoryBytes = 0
	profile.MemoryLimit = 0
	if runtime.GOOS == "windows" {
		profile.RLimitFiles = 8192
	}
}
