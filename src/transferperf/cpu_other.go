//go:build !linux && !darwin && !windows

package transferperf

import "time"

func processCPUTime() (time.Duration, bool) {
	return 0, false
}
