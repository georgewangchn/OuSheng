package store

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"ousheng/internal/card"
)

var ErrConflict = errors.New("version conflict")

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

type Store struct {
	Dir         string
	SignCommits bool
}

func Open(dir string) *Store {
	s := &Store{Dir: dir}
	if os.Getenv("OUSHENG_SIGN_COMMITS") == "1" {
		s.SignCommits = true
	}
	return s
}

func gitRun(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

func (s *Store) cardsDir() string { return filepath.Join(s.Dir, "cards") }
func (s *Store) path(id string) (string, error) {
	if !idRe.MatchString(id) {
		return "", fmt.Errorf("invalid card id %q", id)
	}
	return filepath.Join(s.cardsDir(), id+".yaml"), nil
}

func (s *Store) Init() error {
	if err := os.MkdirAll(s.cardsDir(), 0o755); err != nil {
		return err
	}
	if _, err := gitRun(s.Dir, "init", "-b", "main"); err != nil {
		if _, err2 := gitRun(s.Dir, "init"); err2 != nil {
			return err
		}
	}
	if _, err := gitRun(s.Dir, "config", "user.email", "board@ousheng.local"); err != nil {
		return fmt.Errorf("set user.email: %w", err)
	}
	if _, err := gitRun(s.Dir, "config", "user.name", "board"); err != nil {
		return fmt.Errorf("set user.name: %w", err)
	}
	return nil
}

func (s *Store) List() ([]card.Card, error) {
	entries, err := os.ReadDir(s.cardsDir())
	if err != nil {
		return nil, err
	}
	var out []card.Card
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.cardsDir(), e.Name()))
		if err != nil {
			return nil, err
		}
		c, err := card.Decode(b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		out = append(out, c)
	}
	return out, nil
}

func (s *Store) Get(id string) (card.Card, []byte, bool, error) {
	p, err := s.path(id)
	if err != nil {
		return card.Card{}, nil, false, err
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return card.Card{}, nil, false, nil
	}
	if err != nil {
		return card.Card{}, nil, false, err
	}
	c, err := card.Decode(b)
	if err != nil {
		return card.Card{}, nil, false, err
	}
	return c, b, true, nil
}

func (s *Store) lock() (func(), error) {
	lp := filepath.Join(s.Dir, ".board.lock")
	f, err := os.OpenFile(lp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("board locked: %w", err)
	}
	return func() { f.Close(); os.Remove(lp) }, nil
}

func (s *Store) Write(c card.Card, expectedVersion int, validate func(card.Card, []byte) error, msg string) (card.Card, error) {
	unlock, err := s.lock()
	if err != nil {
		return card.Card{}, err
	}
	defer unlock()

	cur, _, ok, err := s.Get(c.ID)
	if err != nil {
		return card.Card{}, err
	}
	curVer := 0
	if ok {
		curVer = cur.Version
	}
	if expectedVersion != curVer {
		return card.Card{}, ErrConflict
	}
	c.Version = curVer + 1
	out, err := card.Encode(c)
	if err != nil {
		return card.Card{}, err
	}
	if validate != nil {
		if err := validate(c, out); err != nil {
			return card.Card{}, err
		}
	}
	commitArgs := []string{"commit", "-m", msg}
	if s.SignCommits {
		commitArgs = append(commitArgs, "-S")
	}
	if err := s.commitRaw(c.ID, out, commitArgs); err != nil {
		return card.Card{}, err
	}
	return c, nil
}

func (s *Store) commitRaw(id string, raw []byte, commitArgs []string) error {
	p, err := s.path(id)
	if err != nil {
		return err
	}
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		return err
	}
	if _, err := gitRun(s.Dir, "add", filepath.Join("cards", id+".yaml")); err != nil {
		return err
	}
	_, err = gitRun(s.Dir, commitArgs...)
	return err
}

func (s *Store) LastCommitTime(id string) (time.Time, error) {
	out, err := gitRun(s.Dir, "log", "-1", "--format=%ci", filepath.Join("cards", id+".yaml"))
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse("2006-01-02 15:04:05 -0700", strings.TrimSpace(out))
}

func (s *Store) VerifySignatures() ([]string, error) {
	out, err := gitRun(s.Dir, "log", "--all", "--pretty=format:%H %G?", "--", "cards/")
	if err != nil {
		return nil, err
	}
	var problems []string
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		hash, sig := parts[0], parts[1]
		switch sig {
		case "G", "U":
		case "N", "":
			problems = append(problems, hash+": unsigned commit")
		case "B":
			problems = append(problems, hash+": bad signature")
		case "X":
			problems = append(problems, hash+": expired signature")
		case "Y":
			problems = append(problems, hash+": signature by expired key")
		case "R":
			problems = append(problems, hash+": REVOKED signature")
		default:
			problems = append(problems, hash+": signature issue ("+sig+")")
		}
	}
	return problems, nil
}

func (s *Store) GitLog(id string, n int) (string, error) {
	count := fmt.Sprintf("-%d", n)
	return gitRun(s.Dir, "log", count, "--oneline", "--", filepath.Join("cards", id+".yaml"))
}
