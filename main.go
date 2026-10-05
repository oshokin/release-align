// Package main runs the release-align command.
package main

import (
	"os"

	"github.com/oshokin/release-align/cmd"
)

func main() {
	os.Exit(cmd.Execute(os.Args[1:], os.Stdout, os.Stderr))
}
