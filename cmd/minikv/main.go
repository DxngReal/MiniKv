// Command minikv is the single binary for the MiniKV persistent
// key-value store. It hosts the HTTP server and the client subcommands
// (set, get, delete, keys, status, snapshot).
//
// Phase 1 wires up module structure, logging, configuration, and version
// output only. The server and client subcommands arrive in later phases;
// unknown or missing subcommands exit non-zero with usage help.
package main

import (
	"fmt"
	"os"

	"minikv/internal/version"
)

const usage = `minikv — a small persistent concurrent key-value store

Usage:

  minikv <command> [flags]

Commands:

  server     run the HTTP server            (Phase 4)
  set        store a value under a key      (Phase 4)
  get        read a value                   (Phase 4)
  delete     remove a key                   (Phase 4)
  keys       list all live keys             (Phase 4)
  status     show server status             (Phase 4)
  snapshot   create a snapshot              (Phase 4)
  version    print the version
  help       show this help

Examples:

  minikv version
  minikv server --addr 127.0.0.1:8080 --data ./data
`

func main() {
	os.Exit(run(os.Args[1:]))
}

// run executes a subcommand and returns the process exit code. Keeping the
// logic in a function (instead of os.Exit directly) makes exit codes testable.
func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}

	switch args[0] {
	case "version", "--version", "-v":
		fmt.Printf("minikv %s\n", version.Version)
		return 0
	case "help", "--help", "-h":
		fmt.Print(usage)
		return 0
	case "server", "set", "get", "delete", "keys", "status", "snapshot":
		fmt.Fprintf(os.Stderr, "minikv: command %q is not implemented yet (planned for Phase 4)\n", args[0])
		fmt.Fprint(os.Stderr, usage)
		return 1
	default:
		fmt.Fprintf(os.Stderr, "minikv: unknown command %q\n\n", args[0])
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
}
