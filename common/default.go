package common

import (
	"fmt"
	"github.com/fatih/structs"
	"reflect"
	"strconv"
)

func ApplyDefaultValues(struct_ interface{}) (err error) {
	o := structs.New(struct_)

	for _, field := range o.Fields() {
		defaultValue := field.Tag("default")
		if defaultValue == "" {
			continue
		}
		var val interface{}
		switch field.Kind() {
		case reflect.String:
			val = defaultValue
		case reflect.Bool:
			if defaultValue == "true" {
				val = true
			} else if defaultValue == "false" {
				val = false
			} else {
				return fmt.Errorf("invalid bool expression: %v, use true/false", defaultValue)
			}
		case reflect.Int:
			val, err = strconv.Atoi(defaultValue)
			if err != nil {
				return err
			}
		default:
			val = field.Value()
		}
		field.Set(val)
	}
	return nil
}
