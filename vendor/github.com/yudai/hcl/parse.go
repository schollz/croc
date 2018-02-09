package hcl

import (
	"fmt"

	"github.com/yudai/hcl/hcl"
	"github.com/yudai/hcl/json"
)

// Parse parses the given input and returns the root object.
//
// The input format can be either HCL or JSON.
func Parse(input string) (*hcl.Object, error) {
	switch lexMode(input) {
	case lexModeHcl:
		return hcl.Parse(input)
	case lexModeJson:
		return json.Parse(input)
	}

	return nil, fmt.Errorf("unknown config format")
}
