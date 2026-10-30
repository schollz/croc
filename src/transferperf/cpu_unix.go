//go:build linux || darwin

package transferperf

import (
	"time"

	"golang.org/x/sys/unix"
)

func processCPUTime() (time.Duration, bool) {
	var usage unix.Rusage
	if err := unix.Getrusage(unix.RUSAGE_SELF, &usage); err != nil {
		return 0, false
	}
	seconds := usage.Utime.Sec + usage.Stime.Sec
	micros := usage.Utime.Usec + usage.Stime.Usec
	return time.Duration(seconds)*time.Second + time.Duration(micros)*time.Microsecond, true
}
