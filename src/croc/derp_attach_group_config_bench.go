//go:build croc_attach_bench

package croc

import (
	"strconv"
	"strings"
	"time"

	"github.com/schollz/croc/v11/src/derptransport"
)

var (
	derpAttachGroupBuildEnable   = "true"
	derpAttachGroupBuildStreams  = "8"
	derpAttachGroupBuildRawPaths = "4"
	derpAttachGroupBuildBudgetMS = "3000"
	derpAttachGroupBuildRelay    = "false"
)

func derpAttachGroupBuildEnabled() bool {
	enabled, err := strconv.ParseBool(strings.TrimSpace(derpAttachGroupBuildEnable))
	return err == nil && enabled
}

func derpAttachGroupBuildConfig() derptransport.GroupConfig {
	cfg := derptransport.DefaultGroupConfig()
	cfg.StreamCount = parseAttachGroupBuildInt(derpAttachGroupBuildStreams, cfg.StreamCount, 1, 16)
	cfg.MaxRawPaths = parseAttachGroupBuildInt(derpAttachGroupBuildRawPaths, cfg.MaxRawPaths, 1, cfg.StreamCount)
	budgetMS := parseAttachGroupBuildInt(derpAttachGroupBuildBudgetMS, 3000, 1, 30000)
	cfg.RawDirectBudget = time.Duration(budgetMS) * time.Millisecond
	forceRelay, err := strconv.ParseBool(strings.TrimSpace(derpAttachGroupBuildRelay))
	cfg.ForceRelay = err == nil && forceRelay
	return cfg
}

func parseAttachGroupBuildInt(raw string, fallback, minimum, maximum int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < minimum || value > maximum {
		return fallback
	}
	return value
}
