package main

import (
	"fmt"
	"os"
)

var version = "0.0.3-dev"

func main() {
	cmd := "run"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "run":
		os.Exit(run())
	case "version", "--version":
		fmt.Println("orbita-cli", version)
	case "health":
		os.Exit(health())
	case "dns", "config", "status", "queue", "test-mail":
		os.Exit(runTool(cmd, os.Args[2:]))
	default:
		fmt.Fprintf(os.Stderr, "uso: orbita [run|dns|config|status|health|queue|test-mail <destino> [remetente]|version]\n")
		os.Exit(2)
	}
}
