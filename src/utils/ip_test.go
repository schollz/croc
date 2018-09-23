package utils

import (
	"fmt"
	"testing"
)

func TestGetIP(t *testing.T) {
	fmt.Println(PublicIP())
	fmt.Println(LocalIP())
}
