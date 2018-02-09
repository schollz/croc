// +build darwin dragonfly freebsd linux netbsd openbsd solaris

package cli

import "os"

func clearenv() {
	os.Clearenv()
}
