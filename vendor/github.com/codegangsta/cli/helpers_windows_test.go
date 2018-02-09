package cli

import (
	"os"
	"syscall"
)

// os.Clearenv() doesn't actually unset variables on Windows
// See: https://github.com/golang/go/issues/17902
func clearenv() {
	for _, s := range os.Environ() {
		for j := 1; j < len(s); j++ {
			if s[j] == '=' {
				keyp, _ := syscall.UTF16PtrFromString(s[0:j])
				syscall.SetEnvironmentVariable(keyp, nil)
				break
			}
		}
	}
}
