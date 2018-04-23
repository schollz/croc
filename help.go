package main

var helpTemplate = `
                                ,_
                               >' )
                               ( ( \
                                || \
                 /^^^^\         ||
    /^^\________/0     \        ||
   (                    ` + "`" + `~+++,,_||__,,++~^^^^^^^
 ...V^V^V^V^V^V^\...............................


NAME:
   {{.Name}} - {{.Usage}}

USAGE:
   {{.Name}} [options]

VERSION:
   {{.Version}}{{if or .Author .Email}}

AUTHOR:{{if .Author}}
  {{.Author}}{{if .Email}} - <{{.Email}}>{{end}}{{else}}
  {{.Email}}{{end}}{{end}}

OPTIONS:
   {{range .Flags}}{{.}}
   {{end}}
`
