//go:build !croc_tailcat_bench

package croc

import "github.com/schollz/croc/v11/src/tailcattransport"

// Match the established direct transport's parallelism. A single Tailcat TCP
// stream left high-latency direct paths substantially underutilized.
const defaultTailcatStreamCount = 8

func tailcatBuildConfig() tailcattransport.Config {
	return tailcattransport.Config{StreamCount: defaultTailcatStreamCount}
}
