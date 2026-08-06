package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

var helpCommand = &Command{
	Name:      "help",
	Aliases:   []string{"h"},
	Usage:     "Shows a list of commands or help for one command",
	ArgsUsage: "[command]",
	Action: func(c *Context) error {
		args := c.Args()
		if args.Present() {
			return ShowCommandHelp(c, args.First())
		}

		_ = ShowAppHelp(c)
		return nil
	},
}

var helpSubcommand = &Command{
	Name:      "help",
	Aliases:   []string{"h"},
	Usage:     "Shows a list of commands or help for one command",
	ArgsUsage: "[command]",
	Action: func(c *Context) error {
		args := c.Args()
		if args.Present() {
			return ShowCommandHelp(c, args.First())
		}

		return ShowSubcommandHelp(c)
	},
}

// Prints help for the App or Command
type helpPrinter func(w io.Writer, templ string, data interface{})

// Prints help for the App or Command with custom template function.
type helpPrinterCustom func(w io.Writer, templ string, data interface{}, customFunc map[string]interface{})

// HelpPrinter is a function that writes the help output. If not set explicitly,
// this calls HelpPrinterCustom using only the default template functions.
//
// If custom logic for printing help is required, this function can be
// overridden. If the ExtraInfo field is defined on an App, this function
// should not be modified, as HelpPrinterCustom will be used directly in order
// to capture the extra information.
var HelpPrinter helpPrinter = printHelp

// HelpPrinterCustom is a function that writes the help output. It is used as
// the default implementation of HelpPrinter, and may be called directly if
// the ExtraInfo field is set on an App.
var HelpPrinterCustom helpPrinterCustom = printHelpCustom

// VersionPrinter prints the version for the App
var VersionPrinter = printVersion

// ShowAppHelpAndExit - Prints the list of subcommands for the app and exits with exit code.
func ShowAppHelpAndExit(c *Context, exitCode int) {
	_ = ShowAppHelp(c)
	os.Exit(exitCode)
}

// ShowAppHelp is an action that displays the help.
func ShowAppHelp(c *Context) error {
	template := c.App.CustomAppHelpTemplate
	if template == "" {
		template = AppHelpTemplate
	}

	if c.App.ExtraInfo == nil {
		HelpPrinter(c.App.Writer, template, c.App)
		return nil
	}

	customAppData := func() map[string]interface{} {
		return map[string]interface{}{
			"ExtraInfo": c.App.ExtraInfo,
		}
	}
	HelpPrinterCustom(c.App.Writer, template, c.App, customAppData())

	return nil
}

// DefaultAppComplete prints the list of subcommands as the default app completion method
func DefaultAppComplete(c *Context) {
	DefaultCompleteWithFlags(nil)(c)
}

func printCommandSuggestions(commands []*Command, writer io.Writer) {
	for _, command := range commands {
		if command.Hidden {
			continue
		}
		if os.Getenv("_CLI_ZSH_AUTOCOMPLETE_HACK") == "1" {
			for _, name := range command.Names() {
				_, _ = fmt.Fprintf(writer, "%s:%s\n", name, command.Usage)
			}
		} else {
			for _, name := range command.Names() {
				_, _ = fmt.Fprintf(writer, "%s\n", name)
			}
		}
	}
}

func cliArgContains(flagName string) bool {
	for _, name := range strings.Split(flagName, ",") {
		name = strings.TrimSpace(name)
		count := utf8.RuneCountInString(name)
		if count > 2 {
			count = 2
		}
		flag := fmt.Sprintf("%s%s", strings.Repeat("-", count), name)
		for _, a := range os.Args {
			if a == flag {
				return true
			}
		}
	}
	return false
}

func printFlagSuggestions(lastArg string, flags []Flag, writer io.Writer) {
	cur := strings.TrimPrefix(lastArg, "-")
	cur = strings.TrimPrefix(cur, "-")
	for _, flag := range flags {
		if bflag, ok := flag.(*BoolFlag); ok && bflag.Hidden {
			continue
		}
		for _, name := range flag.Names() {
			name = strings.TrimSpace(name)
			// this will get total count utf8 letters in flag name
			count := utf8.RuneCountInString(name)
			if count > 2 {
				count = 2 // resuse this count to generate single - or -- in flag completion
			}
			// if flag name has more than one utf8 letter and last argument in cli has -- prefix then
			// skip flag completion for short flags example -v or -x
			if strings.HasPrefix(lastArg, "--") && count == 1 {
				continue
			}
			// match if last argument matches this flag and it is not repeated
			if strings.HasPrefix(name, cur) && cur != name && !cliArgContains(name) {
				flagCompletion := fmt.Sprintf("%s%s", strings.Repeat("-", count), name)
				_, _ = fmt.Fprintln(writer, flagCompletion)
			}
		}
	}
}

func DefaultCompleteWithFlags(cmd *Command) func(c *Context) {
	return func(c *Context) {
		if len(os.Args) > 2 {
			lastArg := os.Args[len(os.Args)-2]
			if strings.HasPrefix(lastArg, "-") {
				if cmd != nil {
					printFlagSuggestions(lastArg, cmd.Flags, c.App.Writer)
				} else {
					printFlagSuggestions(lastArg, c.App.Flags, c.App.Writer)
				}
				return
			}
		}
		if cmd != nil {
			printCommandSuggestions(cmd.Subcommands, c.App.Writer)
		} else {
			printCommandSuggestions(c.App.Commands, c.App.Writer)
		}
	}
}

// ShowCommandHelpAndExit - exits with code after showing help
func ShowCommandHelpAndExit(c *Context, command string, code int) {
	_ = ShowCommandHelp(c, command)
	os.Exit(code)
}

// ShowCommandHelp prints help for the given command
func ShowCommandHelp(ctx *Context, command string) error {
	// show the subcommand help for a command with subcommands
	if command == "" {
		HelpPrinter(ctx.App.Writer, SubcommandHelpTemplate, ctx.App)
		return nil
	}

	for _, c := range ctx.App.Commands {
		if c.HasName(command) {
			templ := c.CustomHelpTemplate
			if templ == "" {
				templ = CommandHelpTemplate
			}

			HelpPrinter(ctx.App.Writer, templ, c)

			return nil
		}
	}

	if ctx.App.CommandNotFound == nil {
		return Exit(fmt.Sprintf("No help topic for '%v'", command), 3)
	}

	ctx.App.CommandNotFound(ctx, command)
	return nil
}

// ShowSubcommandHelp prints help for the given subcommand
func ShowSubcommandHelp(c *Context) error {
	if c == nil {
		return nil
	}

	if c.Command != nil {
		return ShowCommandHelp(c, c.Command.Name)
	}

	return ShowCommandHelp(c, "")
}

// ShowVersion prints the version number of the App
func ShowVersion(c *Context) {
	VersionPrinter(c)
}

func printVersion(c *Context) {
	_, _ = fmt.Fprintf(c.App.Writer, "%v version %v\n", c.App.Name, c.App.Version)
}

// ShowCompletions prints the lists of commands within a given context
func ShowCompletions(c *Context) {
	a := c.App
	if a != nil && a.BashComplete != nil {
		a.BashComplete(c)
	}
}

// ShowCommandCompletions prints the custom completions for a given command
func ShowCommandCompletions(ctx *Context, command string) {
	c := ctx.App.Command(command)
	if c != nil {
		if c.BashComplete != nil {
			c.BashComplete(ctx)
		} else {
			DefaultCompleteWithFlags(c)(ctx)
		}
	}

}

// printHelpCustom renders the framework's built-in help without text/template.
// Dynamic template method calls prevent the Go linker from eliminating unused
// methods throughout the entire binary. croc only uses the built-in layouts, so
// a direct renderer keeps the same output while allowing method dead-code
// elimination. templ is still used to distinguish app and subcommand layouts;
// custom templates and functions are intentionally unsupported by this fork.
func printHelpCustom(out io.Writer, templ string, data interface{}, customFuncs map[string]interface{}) {
	switch value := data.(type) {
	case *App:
		if templ == SubcommandHelpTemplate {
			renderSubcommandHelp(out, value)
		} else {
			renderAppHelp(out, value)
		}
	case *Command:
		renderCommandHelp(out, value)
	default:
		_, _ = fmt.Fprint(out, templ)
	}
}

func renderAppHelp(w io.Writer, app *App) {
	_, _ = fmt.Fprintln(w, "NAME:")
	_, _ = fmt.Fprintf(w, "   %s", app.Name)
	if app.Usage != "" {
		_, _ = fmt.Fprintf(w, " - %s", app.Usage)
	}
	_, _ = fmt.Fprintln(w)

	_, _ = fmt.Fprintln(w, "\nUSAGE:")
	if app.UsageText != "" {
		_, _ = fmt.Fprintf(w, "   %s\n", app.UsageText)
	} else {
		_, _ = fmt.Fprintf(w, "   %s", app.HelpName)
		if len(app.VisibleFlags()) > 0 {
			_, _ = fmt.Fprint(w, " [global options]")
		}
		if len(app.Commands) > 0 {
			_, _ = fmt.Fprint(w, " command [command options]")
		}
		if app.ArgsUsage != "" {
			_, _ = fmt.Fprintf(w, " %s\n", app.ArgsUsage)
		} else {
			_, _ = fmt.Fprintln(w, " [arguments...]")
		}
	}

	if app.Version != "" && !app.HideVersion {
		_, _ = fmt.Fprintf(w, "\nVERSION:\n   %s\n", app.Version)
	}
	if app.Description != "" {
		_, _ = fmt.Fprintf(w, "\nDESCRIPTION:\n   %s\n", app.Description)
	}
	if len(app.Authors) > 0 {
		heading := "AUTHOR"
		if len(app.Authors) != 1 {
			heading += "S"
		}
		_, _ = fmt.Fprintf(w, "\n%s:\n", heading)
		for _, author := range app.Authors {
			_, _ = fmt.Fprintf(w, "   %s\n", author)
		}
	}
	if len(app.VisibleCommands()) > 0 {
		_, _ = fmt.Fprintln(w, "\nCOMMANDS:")
		renderCommands(w, app.VisibleCategories())
	}
	if flags := app.VisibleFlags(); len(flags) > 0 {
		_, _ = fmt.Fprintln(w, "\nGLOBAL OPTIONS:")
		renderFlags(w, flags)
	}
	if app.Copyright != "" {
		_, _ = fmt.Fprintf(w, "\nCOPYRIGHT:\n   %s\n", app.Copyright)
	}
}

func renderCommandHelp(w io.Writer, command *Command) {
	_, _ = fmt.Fprintf(w, "NAME:\n   %s - %s\n", command.HelpName, command.Usage)
	_, _ = fmt.Fprintln(w, "\nUSAGE:")
	if command.UsageText != "" {
		_, _ = fmt.Fprintf(w, "   %s\n", command.UsageText)
	} else {
		_, _ = fmt.Fprintf(w, "   %s", command.HelpName)
		if len(command.VisibleFlags()) > 0 {
			_, _ = fmt.Fprint(w, " [command options]")
		}
		if command.ArgsUsage != "" {
			_, _ = fmt.Fprintf(w, " %s\n", command.ArgsUsage)
		} else {
			_, _ = fmt.Fprintln(w, " [arguments...]")
		}
	}
	if command.Category != "" {
		_, _ = fmt.Fprintf(w, "\nCATEGORY:\n   %s\n", command.Category)
	}
	if command.Description != "" {
		_, _ = fmt.Fprintf(w, "\nDESCRIPTION:\n   %s\n", command.Description)
	}
	if flags := command.VisibleFlags(); len(flags) > 0 {
		_, _ = fmt.Fprintln(w, "\nOPTIONS:")
		renderFlags(w, flags)
	}
}

func renderSubcommandHelp(w io.Writer, app *App) {
	_, _ = fmt.Fprintf(w, "NAME:\n   %s - %s\n", app.HelpName, app.Usage)
	_, _ = fmt.Fprintln(w, "\nUSAGE:")
	if app.UsageText != "" {
		_, _ = fmt.Fprintf(w, "   %s\n", app.UsageText)
	} else {
		_, _ = fmt.Fprintf(w, "   %s command", app.HelpName)
		if len(app.VisibleFlags()) > 0 {
			_, _ = fmt.Fprint(w, " [command options]")
		}
		if app.ArgsUsage != "" {
			_, _ = fmt.Fprintf(w, " %s\n", app.ArgsUsage)
		} else {
			_, _ = fmt.Fprintln(w, " [arguments...]")
		}
	}
	if app.Description != "" {
		_, _ = fmt.Fprintf(w, "\nDESCRIPTION:\n   %s\n", app.Description)
	}
	if len(app.VisibleCommands()) > 0 {
		_, _ = fmt.Fprintln(w, "\nCOMMANDS:")
		renderCommands(w, app.VisibleCategories())
	}
	if flags := app.VisibleFlags(); len(flags) > 0 {
		_, _ = fmt.Fprintln(w, "\nOPTIONS:")
		renderFlags(w, flags)
	}
}

func renderCommands(w io.Writer, categories []CommandCategory) {
	for _, category := range categories {
		commands := category.VisibleCommands()
		rows := make([]helpRow, 0, len(commands))
		for _, command := range commands {
			rows = append(rows, helpRow{
				left:  strings.Join(command.Names(), ", "),
				right: command.Usage,
			})
		}
		if category.Name() != "" {
			_, _ = fmt.Fprintf(w, "   %s:\n", category.Name())
			renderHelpRows(w, "     ", rows)
			continue
		}
		renderHelpRows(w, "   ", rows)
	}
}

func renderFlags(w io.Writer, flags []Flag) {
	rows := make([]helpRow, 0, len(flags))
	for _, flag := range flags {
		left, right, _ := strings.Cut(flag.String(), "\t")
		rows = append(rows, helpRow{left: left, right: right})
	}
	renderHelpRows(w, "   ", rows)
}

type helpRow struct {
	left  string
	right string
}

func renderHelpRows(w io.Writer, prefix string, rows []helpRow) {
	width := 0
	for _, row := range rows {
		if rowWidth := utf8.RuneCountInString(row.left); rowWidth > width {
			width = rowWidth
		}
	}
	for _, row := range rows {
		_, _ = fmt.Fprintf(w, "%s%-*s", prefix, width, row.left)
		if row.right != "" {
			_, _ = fmt.Fprintf(w, "  %s", row.right)
		}
		_, _ = fmt.Fprintln(w)
	}
}

func printHelp(out io.Writer, templ string, data interface{}) {
	HelpPrinterCustom(out, templ, data, nil)
}

func checkVersion(c *Context) bool {
	found := false
	for _, name := range VersionFlag.Names() {
		if c.Bool(name) {
			found = true
		}
	}
	return found
}

func checkHelp(c *Context) bool {
	found := false
	for _, name := range HelpFlag.Names() {
		if c.Bool(name) {
			found = true
		}
	}
	return found
}

func checkCommandHelp(c *Context, name string) bool {
	if c.Bool("h") || c.Bool("help") {
		_ = ShowCommandHelp(c, name)
		return true
	}

	return false
}

func checkSubcommandHelp(c *Context) bool {
	if c.Bool("h") || c.Bool("help") {
		_ = ShowSubcommandHelp(c)
		return true
	}

	return false
}

func checkShellCompleteFlag(a *App, arguments []string) (bool, []string) {
	if !a.EnableBashCompletion {
		return false, arguments
	}

	pos := len(arguments) - 1
	lastArg := arguments[pos]

	if lastArg != "--generate-bash-completion" {
		return false, arguments
	}

	return true, arguments[:pos]
}

func checkCompletions(c *Context) bool {
	if !c.shellComplete {
		return false
	}

	if args := c.Args(); args.Present() {
		name := args.First()
		if cmd := c.App.Command(name); cmd != nil {
			// let the command handle the completion
			return false
		}
	}

	ShowCompletions(c)
	return true
}

func checkCommandCompletions(c *Context, name string) bool {
	if !c.shellComplete {
		return false
	}

	ShowCommandCompletions(c, name)
	return true
}
