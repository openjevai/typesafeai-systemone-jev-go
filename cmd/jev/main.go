// Command jev asks TypeSafe System One questions from the command line.
//
// Set TYPESAFE_API_KEY, then run 'jev help' for usage, 'jev models' to list
// models, or 'jev noul|choice|score|ask|batch' to ask questions.
package main

import (
	"os"

	"github.com/mheers/typesafeai-systemone-jev-go/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
