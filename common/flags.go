package common

import (
	"io/ioutil"
	"log"
	"os"
	"reflect"
	"strings"

	"github.com/codegangsta/cli"
	"github.com/fatih/structs"
	"github.com/yudai/hcl"

	"github.com/yudai/gotty/pkg/homedir"
	"gopkg.in/yaml.v2"
)

func GenerateFlags(options ...interface{}) (flags []cli.Flag, mappings map[string]string, err error) {
	mappings = make(map[string]string)

	for _, struct_ := range options {
		o := structs.New(struct_)
		for _, field := range o.Fields() {
			flagName := field.Tag("flagName")
			if flagName == "" {
				continue
			}
			envName := "CROC_" + strings.ToUpper(strings.Join(strings.Split(flagName, "-"), "_"))
			mappings[flagName] = field.Name()

			flagShortName := field.Tag("flagSName")
			if flagShortName != "" {
				flagName += ", " + flagShortName
			}

			flagDescription := field.Tag("flagDescribe")

			switch field.Kind() {
			case reflect.String:
				flags = append(flags, cli.StringFlag{
					Name:   flagName,
					Value:  field.Value().(string),
					Usage:  flagDescription,
					EnvVar: envName,
				})
			case reflect.Bool:
				flags = append(flags, cli.BoolFlag{
					Name:   flagName,
					Usage:  flagDescription,
					EnvVar: envName,
				})
			case reflect.Int:
				flags = append(flags, cli.IntFlag{
					Name:   flagName,
					Value:  field.Value().(int),
					Usage:  flagDescription,
					EnvVar: envName,
				})
			}
		}
	}

	return
}

func ApplyFlags(
	flags []cli.Flag,
	mappingHint map[string]string,
	c *cli.Context,
	options ...interface{},
) {
	objects := make([]*structs.Struct, len(options))
	for i, struct_ := range options {
		objects[i] = structs.New(struct_)
	}

	for flagName, fieldName := range mappingHint {
		if !c.IsSet(flagName) {
			continue
		}
		var field *structs.Field
		var ok bool
		for _, o := range objects {
			field, ok = o.FieldOk(fieldName)
			if ok {
				break
			}
		}
		if field == nil {
			continue
		}
		var val interface{}
		switch field.Kind() {
		case reflect.String:
			val = c.String(flagName)
		case reflect.Bool:
			val = c.Bool(flagName)
		case reflect.Int:
			val = c.Int(flagName)
		}
		field.Set(val)
	}
}

func ApplyConfigFile(filePath string, options ...interface{}) error {
	filePath = homedir.Expand(filePath)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return err
	}

	fileString := []byte{}
	log.Printf("Loading config file at: %s", filePath)
	fileString, err := ioutil.ReadFile(filePath)
	if err != nil {
		return err
	}

	for _, object := range options {
		if err := hcl.Decode(object, string(fileString)); err != nil {
			return err
		}
	}

	return nil
}

func ApplyConfigFileYaml(filePath string, options ...interface{}) error {
	filePath = homedir.Expand(filePath)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return err
	}

	fileString := []byte{}
	log.Printf("Loading config file at: %s", filePath)
	fileString, err := ioutil.ReadFile(filePath)
	if err != nil {
		return err
	}

	for _, object := range options {
		if err := yaml.Unmarshal(fileString, object); err != nil {
			return err
		}

	}

	return nil
}

func SaveConfigFileYaml(filePath string, options ...interface{}) error {
	filePath = homedir.Expand(filePath)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return err
	}

	fd, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer fd.Close()

	for _, object := range options {
		if byteString, err := yaml.Marshal(object); err != nil {
			return err
		} else {
			fd.Write(byteString)
		}
	}

	return nil
}
