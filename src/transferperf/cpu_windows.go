//go:build windows

package transferperf

import (
	"time"

	"golang.org/x/sys/windows"
)

func processCPUTime() (time.Duration, bool) {
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(windows.CurrentProcess(), &creation, &exit, &kernel, &user); err != nil {
		return 0, false
	}
	return time.Duration(kernel.Nanoseconds() + user.Nanoseconds()), true
}
