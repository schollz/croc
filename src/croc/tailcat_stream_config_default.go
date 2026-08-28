//go:build !croc_tailcat_bench

package croc

import "github.com/schollz/croc/v11/src/tailcattransport"

const defaultTailcatStreamCount = 1

func tailcatBuildConfig() tailcattransport.Config {
	return tailcattransport.Config{StreamCount: defaultTailcatStreamCount}
}
