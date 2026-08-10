package main

import (
	"fmt"
	"os"
)

const version = "0.1.0"

func versionString() string { return "ousheng board " + version }

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: board <init|read|write|converge|version>")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version":
		fmt.Println(versionString())
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(2)
	}
}
