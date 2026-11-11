// Command fixpoint turns bug reports into verified fixes: it runs the
// console and the loop that drives Kane CLI and a coding agent until the
// report's check passes.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/Manas-Sehgxl/fixpoint/internal/stub"
)

// Version is the build's version, shown by `fixpoint version`.
const Version = "0.1.0"

const usage = `fixpoint turns bug reports into verified fixes.

Usage:
  fixpoint up [flags]       start the console, the demo shop and the loop
  fixpoint doctor           check Kane CLI, the coding agent, Chrome and the target
  fixpoint init             write a starter fixpoint.json and print the widget snippet
  fixpoint demo reset --yes re-seed the demo shop and clear the data directory
  fixpoint version          print the version

Flags for up:
  --addr <host:port>        console address (default 127.0.0.1:7777)
  --shop-addr <host:port>   demo shop address (default 127.0.0.1:4000)
  --data <dir>              data directory (default .fixpoint)
  --stubs                   use the simulated runners: nothing is really verified
  --autonomy auto|ask       fix straight away, or wait for approval

Settings live in fixpoint.json in the current directory.
`

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Print(usage)
		os.Exit(2)
	}
	switch args[0] {
	case "up":
		os.Exit(runUp(args[1:]))
	case "doctor":
		os.Exit(runDoctor(args[1:]))
	case "init":
		os.Exit(runInit(args[1:]))
	case "demo":
		os.Exit(runDemo(args[1:]))
	case "version", "--version", "-v":
		fmt.Printf("fixpoint %s\n", Version)
	case "stub-kane": // hidden: the simulated Kane CLI
		os.Exit(stub.KaneMain(args[1:]))
	case "stub-agent": // hidden: the simulated coding agent
		os.Exit(stub.AgentMain(args[1:]))
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "fixpoint: unknown command %q\n\n%s", args[0], usage)
		os.Exit(2)
	}
}

// fail prints an error the way the rest of the CLI does and returns the
// exit code.
func fail(err error) int {
	msg := err.Error()
	fmt.Fprintf(os.Stderr, "fixpoint: %s%s\n", strings.ToUpper(msg[:1]), msg[1:])
	return 1
}
