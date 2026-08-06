package cli

import (
	"bytes"
	"fmt"
	"io"
	"strings"
)

// ToFishCompletion creates a fish completion string for the `*App`
// The function errors if either parsing or writing of the string fails.
func (a *App) ToFishCompletion() (string, error) {
	var w bytes.Buffer
	if err := a.writeFishCompletionTemplate(&w); err != nil {
		return "", err
	}
	return w.String(), nil
}

func (a *App) writeFishCompletionTemplate(w io.Writer) error {
	allCommands := []string{}

	// Add global flags
	completions := a.prepareFishFlags(a.VisibleFlags(), allCommands)

	// Add help flag
	if !a.HideHelp {
		completions = append(
			completions,
			a.prepareFishFlags([]Flag{HelpFlag}, allCommands)...,
		)
	}

	// Add version flag
	if !a.HideVersion {
		completions = append(
			completions,
			a.prepareFishFlags([]Flag{VersionFlag}, allCommands)...,
		)
	}

	// Add commands and their flags
	completions = append(
		completions,
		a.prepareFishCommands(a.VisibleCommands(), &allCommands, []string{})...,
	)

	_, err := fmt.Fprintf(w, `# %s fish shell completion

function __fish_%s_no_subcommand --description 'Test if there has been any subcommand yet'
    for i in (commandline -opc)
        if contains -- $i%s
            return 1
        end
    end
    return 0
end

`, a.Name, a.Name, prefixedValues(allCommands))
	if err != nil {
		return err
	}
	for _, completion := range completions {
		if _, err = fmt.Fprintln(w, completion); err != nil {
			return err
		}
	}
	return nil
}

func prefixedValues(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return " " + strings.Join(values, " ")
}

func (a *App) prepareFishCommands(commands []*Command, allCommands *[]string, previousCommands []string) []string {
	completions := []string{}
	for _, command := range commands {
		if command.Hidden {
			continue
		}

		var completion strings.Builder
		completion.WriteString(fmt.Sprintf(
			"complete -r -c %s -n '%s' -a '%s'",
			a.Name,
			a.fishSubcommandHelper(previousCommands),
			strings.Join(command.Names(), " "),
		))

		if command.Usage != "" {
			completion.WriteString(fmt.Sprintf(" -d '%s'",
				escapeSingleQuotes(command.Usage)))
		}

		if !command.HideHelp {
			completions = append(
				completions,
				a.prepareFishFlags([]Flag{HelpFlag}, command.Names())...,
			)
		}

		*allCommands = append(*allCommands, command.Names()...)
		completions = append(completions, completion.String())
		completions = append(
			completions,
			a.prepareFishFlags(command.Flags, command.Names())...,
		)

		// recursevly iterate subcommands
		if len(command.Subcommands) > 0 {
			completions = append(
				completions,
				a.prepareFishCommands(
					command.Subcommands, allCommands, command.Names(),
				)...,
			)
		}
	}

	return completions
}

func (a *App) prepareFishFlags(flags []Flag, previousCommands []string) []string {
	completions := []string{}
	for _, f := range flags {
		flag, ok := f.(DocGenerationFlag)
		if !ok {
			continue
		}

		completion := &strings.Builder{}
		completion.WriteString(fmt.Sprintf(
			"complete -c %s -n '%s'",
			a.Name,
			a.fishSubcommandHelper(previousCommands),
		))

		fishAddFileFlag(f, completion)

		for idx, opt := range flag.Names() {
			if idx == 0 {
				completion.WriteString(fmt.Sprintf(
					" -l %s", strings.TrimSpace(opt),
				))
			} else {
				completion.WriteString(fmt.Sprintf(
					" -s %s", strings.TrimSpace(opt),
				))

			}
		}

		if flag.TakesValue() {
			completion.WriteString(" -r")
		}

		if flag.GetUsage() != "" {
			completion.WriteString(fmt.Sprintf(" -d '%s'",
				escapeSingleQuotes(flag.GetUsage())))
		}

		completions = append(completions, completion.String())
	}

	return completions
}

func fishAddFileFlag(flag Flag, completion *strings.Builder) {
	switch f := flag.(type) {
	case *StringFlag:
		if f.TakesFile {
			return
		}
	case *StringSliceFlag:
		if f.TakesFile {
			return
		}
	}
	completion.WriteString(" -f")
}

func (a *App) fishSubcommandHelper(allCommands []string) string {
	fishHelper := fmt.Sprintf("__fish_%s_no_subcommand", a.Name)
	if len(allCommands) > 0 {
		fishHelper = fmt.Sprintf(
			"__fish_seen_subcommand_from %s",
			strings.Join(allCommands, " "),
		)
	}
	return fishHelper

}

func escapeSingleQuotes(input string) string {
	return strings.Replace(input, `'`, `\'`, -1)
}
