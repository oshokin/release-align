// Package main runs the release-align command.
package main

import (
	"os"

	"github.com/oshokin/release-align/cmd"
)

// main runs the CLI and exits with its status code.
func main() {
	os.Exit(cmd.Execute(os.Args[1:], os.Stdout, os.Stderr))
}
