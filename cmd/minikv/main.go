// Command minikv is the single binary for the MiniKV persistent
// key-value store. It hosts the HTTP server and the client subcommands.
//
// Exit codes: 0 success, 1 operation failure, 2 usage error.
package main

import (
	"fmt"
	"io"
	"os"

	"minikv/internal/cli"
	"minikv/internal/version"
)

const usage = `minikv — a small persistent concurrent key-value store

Usage:

  minikv <command> [flags]

Server commands:

  server     run the HTTP server
             --addr 127.0.0.1:8080   listen address
             --data ./data           data directory (WAL + snapshots)
             --durability always     WAL fsync mode (always|never)
             --log-level info        debug|info|warn|error

Client commands (against a running server):

  set        store a value        minikv set KEY VALUE [--ttl 60s]
  get        read a value         minikv get KEY
  delete     remove a key         minikv delete KEY
  keys       list all live keys   minikv keys
  status     show server status   minikv status
  snapshot   write a snapshot     minikv snapshot

Client flags: --addr http://127.0.0.1:8080

Other:

  version    print the version
  help       show this help

Examples:

  minikv server --addr 127.0.0.1:8080 --data ./data
  minikv set greeting hello --ttl 60s
  minikv get greeting
  minikv delete greeting
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run dispatches a subcommand and returns the process exit code. Keeping
// the logic in a function (instead of os.Exit directly) makes it testable.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}

	switch args[0] {
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "minikv %s\n", version.Version)
		return 0
	case "help", "--help", "-h":
		fmt.Fprint(stdout, usage)
		return 0
	case "server":
		return cli.RunServer(args[1:], stderr)
	case "set":
		return cli.RunSet(args[1:], stderr)
	case "get":
		return cli.RunGet(args[1:], stderr)
	case "delete":
		return cli.RunDelete(args[1:], stderr)
	case "keys":
		return cli.RunKeys(args[1:], stderr)
	case "status":
		return cli.RunStatus(args[1:], stderr)
	case "snapshot":
		return cli.RunSnapshot(args[1:], stderr)
	default:
		fmt.Fprintf(stderr, "minikv: unknown command %q\n\n", args[0])
		fmt.Fprint(stderr, usage)
		return 2
	}
}
