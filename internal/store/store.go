package store

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"ousheng/internal/card"
)

var ErrConflict = errors.New("version conflict")

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
func (s *Store) path(id string) string {
	return filepath.Join(s.cardsDir(), id+".yaml")
}

func (s *Store) Init() error {
	if err := os.MkdirAll(s.cardsDir(), 0o755); err != nil {
		return err
	}
	if _, err := gitRun(s.Dir, "init"); err != nil {
		return err
	}
	// 本地身份，保证测试环境可提交
	_, _ = gitRun(s.Dir, "config", "user.email", "board@ousheng.local")
	_, _ = gitRun(s.Dir, "config", "user.name", "board")
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
	b, err := os.ReadFile(s.path(id))
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
	if err := os.WriteFile(s.path(id), raw, 0o644); err != nil {
		return err
	}
	if _, err := gitRun(s.Dir, "add", filepath.Join("cards", id+".yaml")); err != nil {
		return err
	}
	_, err := gitRun(s.Dir, "commit", "-m", msg)
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
