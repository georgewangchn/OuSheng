package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"

	"ousheng/internal/board"
	"ousheng/internal/card"
)

const version = "0.1.0"

func versionString() string { return "ousheng board " + version }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: board <init|read|write|converge|deprecate|context|verify|version>")
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
			b, err := card.Encode(c)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
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
		watch := fs.Bool("watch", false, "watch mode: poll and print on change")
		interval := fs.Int("interval", 5, "poll interval in seconds (watch mode)")
		stuckAfter := fs.String("stuck-after", "", "time-based stuck threshold (e.g. 24h, empty=disabled)")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		opts := board.ConvergeOptions{}
		if *stuckAfter != "" {
			d, err := time.ParseDuration(*stuckAfter)
			if err != nil {
				fmt.Fprintln(stderr, "invalid --stuck-after:", err)
				return 2
			}
			opts.StuckAfter = d
		}
		if *watch {
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()
			return watchConverge(ctx, stdout, stderr, *dir, opts, time.Duration(*interval)*time.Second)
		}
		res, err := board.New(*dir).ConvergeWithOpts(opts)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		out, err := yaml.Marshal(res)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprint(stdout, string(out))
		return 0
	case "deprecate":
		fs := flag.NewFlagSet("deprecate", flag.ContinueOnError)
		dir := fs.String("dir", ".", "board dir")
		expect := fs.Int("expect", 0, "expected version (CAS)")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		if fs.NArg() < 1 {
			fmt.Fprintln(stderr, "usage: board deprecate <id> -expect <version>")
			return 2
		}
		written, dependents, err := board.New(*dir).Deprecate(fs.Arg(0), *expect)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "deprecated %s version %d\n", written.ID, written.Version)
		if len(dependents) > 0 {
			fmt.Fprintln(stdout, "cards needing migration:")
			for _, d := range dependents {
				fmt.Fprintf(stdout, "  - %s\n", d)
			}
		}
		return 0
	case "context":
		fs := flag.NewFlagSet("context", flag.ContinueOnError)
		dir := fs.String("dir", ".", "board dir")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		if fs.NArg() < 1 {
			fmt.Fprintln(stderr, "usage: board context <id>")
			return 2
		}
		c, gitLog, err := board.New(*dir).Context(fs.Arg(0))
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		encoded, err := card.Encode(c)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "--- card ---\n%s", encoded)
		if gitLog != "" {
			fmt.Fprintf(stdout, "--- history (last 10 commits) ---\n%s", gitLog)
		}
		return 0
	case "verify":
		fs := flag.NewFlagSet("verify", flag.ContinueOnError)
		dir := fs.String("dir", ".", "board dir")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		problems, err := board.New(*dir).VerifySignatures()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if len(problems) == 0 {
			fmt.Fprintln(stdout, "all commits signed")
			return 0
		}
		for _, p := range problems {
			fmt.Fprintln(stdout, p)
		}
		return 1
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		return 2
	}
}

func watchConverge(ctx context.Context, stdout, stderr io.Writer, dir string, opts board.ConvergeOptions, interval time.Duration) int {
	var lastStatus string
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return 0
		default:
		}
		res, err := board.New(dir).ConvergeWithOpts(opts)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		current := string(res.Status)
		if current != lastStatus {
			out, err := yaml.Marshal(res)
			if err != nil {
				fmt.Fprintln(stderr, "marshal error:", err)
				return 1
			}
			fmt.Fprintf(stdout, "[%s] %s", time.Now().Format("15:04:05"), string(out))
			lastStatus = current
		}
		<-ticker.C
	}
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
