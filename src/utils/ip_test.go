package utils

import (
	"fmt"
	"testing"
)

func TestGetIP(t *testing.T) {
	log.Debugln(PublicIP())
	log.Debugln(LocalIP())
}
