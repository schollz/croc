package croc

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	humanize "github.com/dustin/go-humanize"
)

func promptCodePhrase() string {
	return getInput("Enter receive code: ")
}

func promptOkayToRecieve(f FileMetaData) (ok bool) {
	overwritingOrReceiving := "Receiving"
	if exists(f.Name) {
		overwritingOrReceiving = "Overwriting"
	}
	fileOrFolder := "file"
	if f.IsDir {
		fileOrFolder = "folder"
	}
	return "y" == getInput(fmt.Sprintf(
		`%s %s (%s) into: %s
ok? (y/N): `,
		overwritingOrReceiving,
		fileOrFolder,
		humanize.Bytes(uint64(f.Size)),
		f.Name,
	))
}

func showIntro(code string, f FileMetaData) {
	fileOrFolder := "file"
	if f.IsDir {
		fileOrFolder = "folder"
	}
	fmt.Fprintf(os.Stderr,
		`Sending %s %s named '%s'
Code is: %s
On the other computer, please run:

croc %s
`,
		humanize.Bytes(uint64(f.Size)),
		fileOrFolder,
		f.Name,
		code,
		code,
	)
}

func getInput(prompt string) string {
	reader := bufio.NewReader(os.Stdin)
	fmt.Fprintf(os.Stderr, "%s", prompt)
	text, _ := reader.ReadString('\n')
	return strings.TrimSpace(text)
}
