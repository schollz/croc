package main

import (
	"fmt"
	"io/ioutil"
	"strings"
)

func main() {
	err := run()
	if err != nil {
		fmt.Println(err)
	}
}

func run() (err error) {
	b, err := ioutil.ReadFile("README.md")
	if err != nil {
		return
	}
	newVersion := GetStringInBetween(string(b), "version-", "-b")

	b, err = ioutil.ReadFile("src/cli/cli.go")
	if err != nil {
		return
	}
	oldVersion := GetStringInBetween(string(b), `Version = "`, `"`)

	if newVersion != oldVersion {
		fmt.Printf("new version: '%s'\n", newVersion)
		fmt.Printf("old version: '%s'\n", oldVersion)
		newCli := strings.Replace(string(b), fmt.Sprintf(`Version = "%s"`, oldVersion), fmt.Sprintf(`Version = "%s"`, newVersion), 1)
		err = ioutil.WriteFile("src/cli/cli.go", []byte(newCli), 0644)
	} else {
		fmt.Printf("current version: '%s'\n", oldVersion)
	}
	return
}

// GetStringInBetween Returns empty string if no start string found
func GetStringInBetween(str string, start string, end string) (result string) {
	s := strings.Index(str, start)
	if s == -1 {
		return
	}
	s += len(start)
	e := strings.Index(str[s:], end)
	if e == -1 {
		return
	}
	e += s
	return str[s:e]
}
