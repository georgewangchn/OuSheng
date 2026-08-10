package store

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"ousheng/internal/card"
)

var ErrConflict = errors.New("version conflict")

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

type Store struct{ Dir string }

func Open(dir string) *Store { return &Store{Dir: dir} }

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

func (s *Store) commit(id string, raw []byte, msg string) error {
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
	_, err = gitRun(s.Dir, "commit", "-m", msg)
	return err
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
	if err := s.commit(c.ID, out, msg); err != nil {
		return card.Card{}, err
	}
	return c, nil
}
