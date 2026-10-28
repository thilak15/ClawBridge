package main

import (
	"os"

	"clawbridge/cmd/clawbridge/cli"
)

func main() {
	exitCode := cli.Execute(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	os.Exit(exitCode)
}
