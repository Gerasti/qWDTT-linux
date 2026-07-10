package main

import (
	"fmt"
	"log"
	"os"
)

const version = "0.0.1"

func main() {
	log.SetFlags(0)

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "connect", "con":
		connectCmd()
	case "add":
		addCmd()
	case "edit":
		editCmd()
	case "remove", "rm":
		removeCmd()
	case "list", "ls":
		listCmd()
	case "show", "sh":
		showCmd()
	case "device-id", "id":
		deviceIDCmd()
	case "regenerate-id":
		regenerateIDCmd()
	case "version", "--version":
		fmt.Printf("qwdtt-cli v%s\n", version)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}
