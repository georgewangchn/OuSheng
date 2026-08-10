package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"

	"ousheng/internal/board"
	"ousheng/internal/card"
)

const version = "0.1.0"

func versionString() string { return "ousheng board " + version }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: board <init|read|write|converge|version>")
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, versionString())
		return 0
	case "init":
		dir := "."
		if len(args) > 1 {
			dir = args[1]
		}
		if err := board.New(dir).Init(); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	case "read":
		fs := flag.NewFlagSet("read", flag.ContinueOnError)
		dir := fs.String("dir", ".", "board dir")
		owner := fs.String("owner", "", "filter owner")
		status := fs.String("status", "", "filter status")
		kind := fs.String("kind", "", "filter kind")
		id := fs.String("id", "", "filter id")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		cards, err := board.New(*dir).ReadBoard(board.Scope{ID: *id, Owner: *owner, Status: *status, Kind: *kind})
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		for _, c := range cards {
			b, _ := card.Encode(c)
			fmt.Fprintf(stdout, "---\n%s", b)
		}
		return 0
	case "write":
		fs := flag.NewFlagSet("write", flag.ContinueOnError)
		dir := fs.String("dir", ".", "board dir")
		file := fs.String("file", "", "card yaml file")
		expect := fs.Int("expect", 0, "expected version (CAS)")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		raw, err := os.ReadFile(*file)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		c, err := card.Decode(raw)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		w, err := board.New(*dir).WriteBoard(c, *expect)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "written %s version %d\n", w.ID, w.Version)
		return 0
	case "converge":
		fs := flag.NewFlagSet("converge", flag.ContinueOnError)
		dir := fs.String("dir", ".", "board dir")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		res, err := board.New(*dir).Converge()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		out, _ := yaml.Marshal(res)
		fmt.Fprint(stdout, string(out))
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		return 2
	}
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
