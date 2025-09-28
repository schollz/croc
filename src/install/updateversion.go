package main

import (
	"fmt"
	"os"
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
	versionNew := "v" + os.Getenv("VERSION")
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
	b, err := os.ReadFile(fname)
	if err != nil {
		return
	}
	oldVersion := getStringInBetween(string(b), start, end)
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
	err = os.WriteFile(fname, []byte(newF), 0o644)
	return
}

// getStringInBetween Returns empty string if no start string found
func getStringInBetween(str, start, end string) (result string) {
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
