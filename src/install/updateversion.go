package main

import (
	"fmt"
	"io/ioutil"
	"os/exec"
	"strings"
)

func main() {
	err := run()
	if err != nil {
		fmt.Println(err)
	}
}

func run() (err error) {
	version, err := exec.Command("git", "describe", "--abbrev=0").Output()
	if err != nil {
		return
	}
	versionNew := strings.TrimSpace(string(version))
	versionHash, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return
	}
	versionHashNew := strings.TrimSpace(string(versionHash))
	fmt.Println(versionNew)
	fmt.Println(versionHashNew)

	err = replaceInFile("src/cli/cli.go", `Version = "`, `"`, versionNew+"-"+versionHashNew)
	if err == nil {
		fmt.Printf("updated cli.go to version %s\n", versionNew)
	}
	err = replaceInFile("README.md", `version-`, `-b`, strings.Split(versionNew, "-")[0])
	if err == nil {
		fmt.Printf("updated README to version %s\n", strings.Split(versionNew, "-")[0])
	}

	err = replaceInFile("src/install/default.txt", `croc_version="`, `"`, strings.Split(versionNew, "-")[0][1:])
	if err == nil {
		fmt.Printf("updated default.txt to version %s\n", strings.Split(versionNew, "-")[0][1:])
	}

	return
}

func replaceInFile(fname, start, end, replacement string) (err error) {
	b, err := ioutil.ReadFile(fname)
	if err != nil {
		return
	}
	oldVersion := GetStringInBetween(string(b), start, end)
	if oldVersion == "" {
		err = fmt.Errorf("nothing")
		return
	}
	newF := strings.Replace(
		string(b),
		fmt.Sprintf("%s%s%s", start, oldVersion, end),
		fmt.Sprintf("%s%s%s", start, replacement, end),
		1,
	)
	err = ioutil.WriteFile(fname, []byte(newF), 0644)
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
