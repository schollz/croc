# croc CLI framework

This package is derived from `github.com/schollz/cli/v2` v2.2.1, which in
turn is based on urfave/cli and is distributed under the MIT license included
in this directory.

It keeps the parsing behavior and the subset of the API used by croc. Help and
fish-completion output are rendered directly instead of through
`text/template`. Dynamic template method calls disable method dead-code
elimination across a Go binary, so avoiding them materially reduces the size
of both `croc` and `croc-web` without removing CLI features.
