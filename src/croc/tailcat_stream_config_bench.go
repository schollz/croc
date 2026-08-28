//go:build croc_tailcat_bench

package croc

import (
	"strconv"
	"strings"

	"github.com/schollz/croc/v11/src/tailcattransport"
)

// tailcatStreamBuildCount is overridden with -ldflags -X for benchmark builds.
var tailcatStreamBuildCount = "1"

func tailcatBuildConfig() tailcattransport.Config {
	count, err := strconv.Atoi(strings.TrimSpace(tailcatStreamBuildCount))
	if err != nil || count < tailcattransport.MinStreamCount || count > tailcattransport.MaxStreamCount {
		count = 1
	}
	return tailcattransport.Config{StreamCount: count}
}
