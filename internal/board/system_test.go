package board

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"ousheng/internal/card"
	"ousheng/internal/store"
)

func realisticCard(id string) card.Card {
	return card.Card{
		ID:      id,
		Owner:   "backend",
		Task:    "提供用户登录鉴权接口",
		Status:  card.Proposed,
		Version: 1,
		Contract: card.Contract{
			Kind:     "http",
			Breaking: false,
			Interface: []any{
				map[string]any{
					"method":   "POST",
					"path":     "/login",
					"behavior": "有效凭证返回 token；无效返回 401",
				},
			},
		},
	}
}

func transitionTo(b *Board, id string, target card.Status, expectedVersion int) (card.Card, error) {
	c, err := b.ReadBoard(Scope{ID: id})
	if err != nil {
		return card.Card{}, err
	}
	if len(c) != 1 {
		return card.Card{}, errors.New("card not found")
	}
	cur := c[0]
	cur.Status = target
	if target == card.Verified {
		cur.Evidence = &card.Evidence{
			Probe:          "test/auth_integration: POST /login 200 + token 可用",
			PassedAtCommit: "abc123",
			By:             "frontend",
		}
	}
	return b.WriteBoard(cur, expectedVersion)
}

func TestSystemFullLifecycleRealistic(t *testing.T) {
	b := New(t.TempDir())
	if err := b.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	c := realisticCard("user-auth-api")
	written, err := b.WriteBoard(c, 0)
	if err != nil {
		t.Fatalf("write proposed: %v", err)
	}
	if written.Version != 1 {
		t.Fatalf("want version 1, got %d", written.Version)
	}

	steps := []card.Status{card.Agreed, card.Live, card.Verified}
	v := written.Version
	for _, s := range steps {
		updated, err := transitionTo(b, "user-auth-api", s, v)
		if err != nil {
			t.Fatalf("transition to %s: %v", s, err)
		}
		if updated.Version != v+1 {
			t.Fatalf("version not bumped on %s: got %d want %d", s, updated.Version, v+1)
		}
		v = updated.Version
	}

	got, err := b.Converge()
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusConverged {
		t.Fatalf("want CONVERGED after full lifecycle, got %s %+v", got.Status, got)
	}
}

func TestSystemC2BreakingChangeLimiter(t *testing.T) {
	b := New(t.TempDir())
	_ = b.Init()

	cases := []struct {
		name     string
		breaking bool
		ack      *card.HumanAck
		wantErr  bool
	}{
		{"breaking without ack", true, nil, true},
		{"breaking with empty approver", true, &card.HumanAck{Approver: "", AtVersion: 1}, true},
		{"breaking with approver", true, &card.HumanAck{Approver: "alice", AtVersion: 1}, false},
		{"non-breaking", false, nil, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := realisticCard("c2-test")
			c.ID = "c2-" + strings.ReplaceAll(tc.name, " ", "-")
			c.Contract.Breaking = tc.breaking
			c.HumanAck = tc.ack
			_, err := b.WriteBoard(c, 0)
			if tc.wantErr && err == nil {
				t.Fatal("expected rejection")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestSystemC1BehavioralSensor(t *testing.T) {
	b := New(t.TempDir())
	_ = b.Init()

	t.Run("verified without evidence rejected", func(t *testing.T) {
		c := realisticCard("c1-noev")
		c.Status = card.Proposed
		if _, err := b.WriteBoard(c, 0); err != nil {
			t.Fatal(err)
		}
		got, _ := b.ReadBoard(Scope{ID: "c1-noev"})
		got[0].Status = card.Verified
		got[0].Evidence = nil
		if _, err := b.WriteBoard(got[0], 1); err == nil {
			t.Fatal("expected rejection: verified without evidence")
		}
	})

	t.Run("verified with incomplete evidence rejected", func(t *testing.T) {
		c := realisticCard("c1-inc")
		if _, err := b.WriteBoard(c, 0); err != nil {
			t.Fatal(err)
		}
		got, _ := b.ReadBoard(Scope{ID: "c1-inc"})
		got[0].Status = card.Verified
		got[0].Evidence = &card.Evidence{Probe: "p", PassedAtCommit: "", By: "frontend"}
		if _, err := b.WriteBoard(got[0], 1); err == nil {
			t.Fatal("expected rejection: incomplete evidence")
		}
	})
}

func TestSystemCASConcurrency(t *testing.T) {
	b := New(t.TempDir())
	_ = b.Init()

	c := realisticCard("cas-race")
	initial, err := b.WriteBoard(c, 0)
	if err != nil {
		t.Fatal(err)
	}
	if initial.Version != 1 {
		t.Fatalf("want initial version 1, got %d", initial.Version)
	}

	const N = 10
	var wg sync.WaitGroup
	wg.Add(N)
	wins := make(chan int, N)
	losers := make(chan int, N)
	readFails := make(chan int, N)
	unexpected := make(chan error, N)

	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			got, err := b.ReadBoard(Scope{ID: "cas-race"})
			if err != nil || len(got) != 1 {
				readFails <- 1
				return
			}
			updated := got[0]
			updated.Task = "updated by goroutine"
			_, err = b.WriteBoard(updated, got[0].Version)
			switch {
			case err == nil:
				wins <- 1
			case errors.Is(err, store.ErrConflict):
				losers <- 1
			default:
				unexpected <- err
			}
		}()
	}
	wg.Wait()
	close(wins)
	close(losers)
	close(readFails)
	close(unexpected)

	if n := len(readFails); n != 0 {
		t.Fatalf("want 0 read failures, got %d", n)
	}
	if n := len(unexpected); n != 0 {
		t.Fatalf("want 0 unexpected errors, got %d: %v", n, <-unexpected)
	}
	winCount := len(wins)
	loserCount := len(losers)
	if winCount < 1 {
		t.Fatalf("want at least 1 winner, got %d", winCount)
	}
	if winCount+loserCount != N {
		t.Fatalf("want %d total outcomes, got %d", N, winCount+loserCount)
	}

	// CAS invariant: no lost updates. Reads happen while writes are in flight,
	// so later goroutines may legitimately win on a newer version; what must
	// hold is that every win bumped the version exactly once.
	final, err := b.ReadBoard(Scope{ID: "cas-race"})
	if err != nil || len(final) != 1 {
		t.Fatalf("read final: %v", err)
	}
	if want := initial.Version + winCount; final[0].Version != want {
		t.Fatalf("no-lost-update violated: want final version %d, got %d", want, final[0].Version)
	}
}

func TestSystemPersistenceAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	b1 := New(dir)
	_ = b1.Init()

	c := realisticCard("persist-test")
	w, err := b1.WriteBoard(c, 0)
	if err != nil {
		t.Fatal(err)
	}

	b2 := New(dir)
	got, err := b2.ReadBoard(Scope{ID: "persist-test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 card after reopen, got %d", len(got))
	}
	if got[0].ID != "persist-test" || got[0].Version != w.Version || got[0].Task != c.Task {
		t.Fatalf("content mismatch after reopen: %+v", got[0])
	}
	if got[0].Contract.Kind != "http" {
		t.Fatalf("contract.kind mismatch: %s", got[0].Contract.Kind)
	}
}

func TestSystemConvergenceMatrix(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, b *Board)
		want  ConvergenceStatus
	}{
		{
			name:  "empty board converged",
			setup: func(t *testing.T, b *Board) {},
			want:  StatusConverged,
		},
		{
			name: "proposed in progress",
			setup: func(t *testing.T, b *Board) {
				_, _ = b.WriteBoard(realisticCard("p1"), 0)
			},
			want: StatusInProgress,
		},
		{
			name: "all verified converged",
			setup: func(t *testing.T, b *Board) {
				c := realisticCard("v1")
				c, _ = b.WriteBoard(c, 0)
				c, _ = transitionTo(b, "v1", card.Agreed, c.Version)
				c, _ = transitionTo(b, "v1", card.Live, c.Version)
				_, _ = transitionTo(b, "v1", card.Verified, c.Version)
			},
			want: StatusConverged,
		},
		{
			name: "cycle stuck",
			setup: func(t *testing.T, b *Board) {
				a := realisticCard("cy-a")
				a.DependsOn = []string{"cy-b"}
				bb := realisticCard("cy-b")
				bb.DependsOn = []string{"cy-a"}
				_, _ = b.WriteBoard(a, 0)
				_, _ = b.WriteBoard(bb, 0)
			},
			want: StatusStuck,
		},
		{
			name: "broken dep stuck",
			setup: func(t *testing.T, b *Board) {
				dep := realisticCard("dep-ok")
				dep, _ = b.WriteBoard(dep, 0)
				dep, _ = transitionTo(b, "dep-ok", card.Agreed, dep.Version)
				dep, _ = transitionTo(b, "dep-ok", card.Live, dep.Version)
				dep, _ = transitionTo(b, "dep-ok", card.Verified, dep.Version)
				_, _ = transitionTo(b, "dep-ok", card.Deprecated, dep.Version)

				consumer := realisticCard("cons-broken")
				consumer.DependsOn = []string{"dep-ok"}
				consumer, _ = b.WriteBoard(consumer, 0)
				consumer, _ = transitionTo(b, "cons-broken", card.Agreed, consumer.Version)
				consumer, _ = transitionTo(b, "cons-broken", card.Live, consumer.Version)
				_, _ = transitionTo(b, "cons-broken", card.Verified, consumer.Version)
			},
			want: StatusStuck,
		},
		{
			name: "dangling dep stuck",
			setup: func(t *testing.T, b *Board) {
				c := realisticCard("dangle")
				c, _ = b.WriteBoard(c, 0)
				c, _ = transitionTo(b, "dangle", card.Agreed, c.Version)
				c, _ = transitionTo(b, "dangle", card.Live, c.Version)
				c.DependsOn = []string{"ghost"}
				last, _ := b.ReadBoard(Scope{ID: "dangle"})
				last[0].DependsOn = []string{"ghost"}
				last[0].Status = card.Verified
				last[0].Evidence = &card.Evidence{Probe: "p", PassedAtCommit: "c", By: "frontend"}
				_, _ = b.WriteBoard(last[0], c.Version)
			},
			want: StatusStuck,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := New(t.TempDir())
			_ = b.Init()
			tc.setup(t, b)
			got, err := b.Converge()
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tc.want {
				t.Fatalf("want %s, got %s %+v", tc.want, got.Status, got)
			}
		})
	}
}

func TestSystemProjectionGateReal(t *testing.T) {
	b := New(t.TempDir())
	_ = b.Init()

	t.Run("oversize rejected", func(t *testing.T) {
		c := realisticCard("big")
		c.Task = strings.Repeat("x", card.MaxCardBytes+100)
		if _, err := b.WriteBoard(c, 0); err == nil {
			t.Fatal("expected oversize rejection")
		}
	})

	t.Run("code block rejected", func(t *testing.T) {
		c := realisticCard("code")
		c.Task = "```\nfunc leak() {}\n```"
		if _, err := b.WriteBoard(c, 0); err == nil {
			t.Fatal("expected code block rejection")
		}
	})

	t.Run("traceback rejected", func(t *testing.T) {
		c := realisticCard("trace")
		c.Task = "Traceback (most recent call last): boom"
		if _, err := b.WriteBoard(c, 0); err == nil {
			t.Fatal("expected traceback rejection")
		}
	})

	t.Run("unknown field rejected on decode", func(t *testing.T) {
		raw := []byte("id: unk\nowner: b\ntask: t\nstatus: proposed\nversion: 1\ncontract:\n  kind: http\n  breaking: false\n  interface: []\nsecret_memory: leak\n")
		_, err := card.Decode(raw)
		if err == nil {
			t.Fatal("expected unknown field rejection")
		}
	})
}
