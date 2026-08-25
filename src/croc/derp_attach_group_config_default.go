//go:build !croc_attach_bench

package croc

import "github.com/schollz/croc/v11/src/derptransport"

func derpAttachGroupBuildEnabled() bool { return false }

func derpAttachGroupBuildConfig() derptransport.GroupConfig {
	return derptransport.DefaultGroupConfig()
}
