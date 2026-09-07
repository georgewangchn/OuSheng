package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"ousheng/internal/index"
	sqliteindex "ousheng/internal/index/sqlite"
	"ousheng/internal/migrate"
	"ousheng/internal/model"
	"ousheng/internal/state/gityaml"
)

// --- index ---

// rebuildIndex 从 canonical YAML 重建派生索引。
// 当前为内存索引 + 报告；Phase 5 SQLite 落盘后此函数一并重建 cache/index.db。
func rebuildIndex(dir string, stdout, stderr io.Writer) int {
	idx, err := loadIndex(dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	n := countOf(idx.All)
	na := countOf(idx.Actors)
	ns := countOf(idx.Systems)
	// SQLite 派生索引（可删除、可重建；S5）
	if err := rebuildSQLite(dir); err != nil {
		fmt.Fprintf(stderr, "sqlite index: %v (memory index still active)\n", err)
	}
	fmt.Fprintf(stdout, "index rebuilt: %d work items, %d actors, %d systems\n", n, na, ns)
	return 0
}

func countOf[T any](f func() ([]T, error)) int {
	v, err := f()
	if err != nil {
		return 0
	}
	return len(v)
}

// rebuildSQLite 重建 .ousheng/cache/index.db（可删除、可重建；S5 硬约束）。
func rebuildSQLite(dir string) error {
	repo := gityaml.Open(dir)
	snap, err := index.Load(repo)
	if err != nil {
		return err
	}
	cacheDir := filepath.Join(dir, ".ousheng", "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	sidx, err := sqliteindex.Open(filepath.Join(cacheDir, "index.db"))
	if err != nil {
		return err
	}
	defer sidx.Close()
	return sidx.Rebuild(snap)
}

func cmdIndex(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: ousheng index <rebuild|status>")
		return 2
	}
	fs := newFS("index " + args[0])
	if err := parseLoose(fs.FlagSet, args[1:]); err != nil {
		return usageErr(stderr, err)
	}
	dir := fs.Dir()
	switch args[0] {
	case "rebuild":
		return rebuildIndex(dir, stdout, stderr)
	case "status":
		repo := gityaml.Open(dir)
		if _, err := repo.GetProject(); err != nil {
			fmt.Fprintf(stderr, "not an ousheng workspace: %v\n", err)
			return 1
		}
		idx, err := loadIndex(dir)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		c, err := ctxService(dir)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "memory index: %d work items, %d actors, %d systems, %d assignments\n",
			countOf(idx.All), countOf(idx.Actors), countOf(idx.Systems), len(assignmentsOf(c)))
		cacheDB := dir + string(os.PathSeparator) + ".ousheng/cache/index.db"
		if _, err := os.Stat(cacheDB); err == nil {
			fmt.Fprintf(stdout, "sqlite index: %s\n", cacheDB)
		} else {
			fmt.Fprintln(stdout, "sqlite index: none (memory index active, zero-infrastructure mode)")
		}
		return 0
	}
	fmt.Fprintln(stderr, "unknown index subcommand:", args[0])
	return 2
}

// --- migrate ---

func cmdMigrate(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: ousheng migrate <schema|resolve-owner>")
		return 2
	}
	fs := newFS("migrate " + args[0])
	switch args[0] {
	case "schema":
		projectID := fs.String("project-id", "migrated", "project id for new workspace")
		projectName := fs.String("project-name", "", "project name")
		if err := parseLoose(fs.FlagSet, args[1:]); err != nil {
			return usageErr(stderr, err)
		}
		dir := fs.Dir()
		repo := gityaml.Open(dir)
		// .ousheng 不存在则先初始化骨架（registries 可后补）
		if _, err := repo.GetProject(); err != nil {
			if err := repo.InitWorkspace(model.Project{ID: *projectID, Name: *projectName}); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
		}
		cards, err := migrate.ReadCards(dir)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if len(cards) == 0 {
			fmt.Fprintln(stdout, "no v1 cards found (cards/*.yaml)")
			return 0
		}
		res, err := migrate.MigrateSchema(repo, cards)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprint(stdout, res.SummaryText())
		return 0

	case "resolve-owner":
		assignee := fs.String("assignee", "", "resolved assignee actor id")
		role := fs.String("role", "", "resolved acting role id")
		accountable := fs.String("accountable", "", "accountable human id")
		actor := fs.String("actor", "", "who performs the resolution")
		if err := parseLoose(fs.FlagSet, args[1:]); err != nil {
			return usageErr(stderr, err)
		}
		if fs.NArg() < 1 {
			fmt.Fprintln(stderr, "usage: ousheng migrate resolve-owner <id> --assignee A [--role R --accountable H]")
			return 2
		}
		repo := gityaml.Open(fs.Dir())
		w, err := migrate.ResolveOwner(repo, fs.Arg(0), *assignee, *role, *accountable, *actor)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "resolved %s: assignee=%s role=%s (revision %d)\n", w.ID, w.Assignee, w.ActingRole, w.Revision)
		return 0
	}
	fmt.Fprintln(stderr, "unknown migrate subcommand:", args[0])
	return 2
}
