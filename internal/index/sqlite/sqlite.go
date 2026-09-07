// Package sqlite 是 Index 的 SQLite 派生索引实现（v0.3 §29/§50）。
//
// 铁律：
//   - 只保存可从 canonical YAML 重建的数据
//   - 非 canonical：可删除（rm .ousheng/cache/index.db）、可重建（ousheng index rebuild）
//   - 不提交 Git（cache/.gitignore 兜底）
//   - 与 memory 实现查询结果必须完全一致（S5 硬约束，见等价性测试）
package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"

	"ousheng/internal/index"
	"ousheng/internal/model"
)

type SQLiteIndex struct {
	db *sql.DB
}

var _ index.Index = (*SQLiteIndex)(nil)

// Open 打开（或创建）index.db。
func Open(path string) (*SQLiteIndex, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// 单机 CLI 场景：串行化写，避免 SQLITE_BUSY
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, err
	}
	return &SQLiteIndex{db: db}, nil
}

func (s *SQLiteIndex) Close() error { return s.db.Close() }

// Rebuild 全量重建：drop + create + insert（保证索引与 canonical 一致）。
func (s *SQLiteIndex) Rebuild(snap index.Snapshot) error {
	if _, err := s.db.Exec(`DROP TABLE IF EXISTS work_items`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DROP TABLE IF EXISTS work_deps`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DROP TABLE IF EXISTS actors`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DROP TABLE IF EXISTS systems`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DROP TABLE IF EXISTS assignments`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DROP TABLE IF EXISTS roles`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DROP TABLE IF EXISTS meta`); err != nil {
		return err
	}

	stmts := []string{
		`CREATE TABLE work_items (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			title TEXT NOT NULL,
			system TEXT DEFAULT '',
			target_version TEXT DEFAULT '',
			assignee TEXT DEFAULT '',
			acting_role TEXT DEFAULT '',
			accountable_human TEXT DEFAULT '',
			detected_by TEXT DEFAULT '',
			status TEXT NOT NULL,
			revision INTEGER NOT NULL,
			payload TEXT NOT NULL
		)`,
		`CREATE INDEX idx_work_assignee ON work_items(assignee)`,
		`CREATE INDEX idx_work_accountable ON work_items(accountable_human)`,
		`CREATE INDEX idx_work_system ON work_items(system)`,
		`CREATE INDEX idx_work_version ON work_items(target_version)`,
		`CREATE INDEX idx_work_status ON work_items(status)`,
		`CREATE INDEX idx_work_type ON work_items(type)`,
		`CREATE TABLE work_deps (
			work_id TEXT NOT NULL,
			dep_id TEXT NOT NULL,
			PRIMARY KEY (work_id, dep_id)
		)`,
		`CREATE TABLE actors (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			display_name TEXT DEFAULT '',
			responsible_human TEXT DEFAULT '',
			payload TEXT NOT NULL
		)`,
		`CREATE TABLE systems (
			id TEXT PRIMARY KEY,
			name TEXT DEFAULT '',
			parent TEXT DEFAULT '',
			payload TEXT NOT NULL
		)`,
		`CREATE TABLE assignments (
			actor TEXT NOT NULL,
			role TEXT NOT NULL,
			system TEXT NOT NULL,
			responsibility TEXT NOT NULL,
			active INTEGER NOT NULL,
			payload TEXT NOT NULL,
			PRIMARY KEY (actor, role, system, responsibility)
		)`,
		`CREATE TABLE roles (
			id TEXT PRIMARY KEY,
			name TEXT DEFAULT '',
			payload TEXT NOT NULL
		)`,
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("create: %w", err)
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	insertWork, err := tx.Prepare(`INSERT INTO work_items
		(id, type, title, system, target_version, assignee, acting_role, accountable_human, detected_by, status, revision, payload)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer insertWork.Close()
	insertDep, err := tx.Prepare(`INSERT OR IGNORE INTO work_deps (work_id, dep_id) VALUES (?,?)`)
	if err != nil {
		return err
	}
	defer insertDep.Close()

	for _, w := range snap.WorkItems {
		payload, err := json.Marshal(w)
		if err != nil {
			return err
		}
		if _, err := insertWork.Exec(w.ID, string(w.Type), w.Title, w.System, w.TargetVersion,
			w.Assignee, w.ActingRole, w.AccountableHuman, w.DetectedBy, string(w.Status), w.Revision, string(payload)); err != nil {
			return err
		}
		for _, dep := range w.DependsOn {
			if _, err := insertDep.Exec(w.ID, dep); err != nil {
				return err
			}
		}
	}

	insertActor, err := tx.Prepare(`INSERT INTO actors (id, type, display_name, responsible_human, payload) VALUES (?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer insertActor.Close()
	for _, af := range snap.Actors {
		payload, err := json.Marshal(af)
		if err != nil {
			return err
		}
		if _, err := insertActor.Exec(af.Actor.ID, string(af.Actor.Type), af.Actor.DisplayName, af.Actor.ResponsibleHuman, string(payload)); err != nil {
			return err
		}
	}

	insertSystem, err := tx.Prepare(`INSERT INTO systems (id, name, parent, payload) VALUES (?,?,?,?)`)
	if err != nil {
		return err
	}
	defer insertSystem.Close()
	for _, sys := range snap.Systems {
		payload, err := json.Marshal(sys)
		if err != nil {
			return err
		}
		if _, err := insertSystem.Exec(sys.ID, sys.Name, sys.Parent, string(payload)); err != nil {
			return err
		}
	}

	insertAssign, err := tx.Prepare(`INSERT OR IGNORE INTO assignments (actor, role, system, responsibility, active, payload) VALUES (?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer insertAssign.Close()
	for _, a := range snap.Assignments {
		payload, err := json.Marshal(a)
		if err != nil {
			return err
		}
		if _, err := insertAssign.Exec(a.Actor, a.Role, a.System, a.Responsibility, boolInt(a.Active), string(payload)); err != nil {
			return err
		}
	}

	insertRole, err := tx.Prepare(`INSERT INTO roles (id, name, payload) VALUES (?,?,?)`)
	if err != nil {
		return err
	}
	defer insertRole.Close()
	for _, r := range snap.Roles {
		payload, err := json.Marshal(r)
		if err != nil {
			return err
		}
		if _, err := insertRole.Exec(r.ID, r.Name, string(payload)); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(`INSERT INTO meta (key, value) VALUES ('rebuilt_at', ?)`, model.Now()); err != nil {
		return err
	}
	return tx.Commit()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// --- 查询 ---

const workCols = `id, type, title, system, target_version, assignee, acting_role, accountable_human, detected_by, status, revision, payload`

func (s *SQLiteIndex) scanWork(where string, args ...any) ([]model.WorkItem, error) {
	q := `SELECT payload FROM work_items` + where
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.WorkItem
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var w model.WorkItem
		if err := json.Unmarshal([]byte(payload), &w); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *SQLiteIndex) Get(id string) (model.WorkItem, bool, error) {
	out, err := s.scanWork(` WHERE id = ?`, id)
	if err != nil {
		return model.WorkItem{}, false, err
	}
	if len(out) == 0 {
		return model.WorkItem{}, false, nil
	}
	return out[0], true, nil
}

func (s *SQLiteIndex) All() ([]model.WorkItem, error) {
	return s.scanWork(` ORDER BY id`)
}

func (s *SQLiteIndex) ByAssignee(actor string) ([]model.WorkItem, error) {
	return s.scanWork(` WHERE assignee = ? ORDER BY id`, actor)
}

func (s *SQLiteIndex) ByAccountable(human string) ([]model.WorkItem, error) {
	return s.scanWork(` WHERE accountable_human = ? ORDER BY id`, human)
}

func (s *SQLiteIndex) BySystem(system string) ([]model.WorkItem, error) {
	return s.scanWork(` WHERE system = ? ORDER BY id`, system)
}

func (s *SQLiteIndex) ByVersion(version string) ([]model.WorkItem, error) {
	return s.scanWork(` WHERE target_version = ? ORDER BY id`, version)
}

func (s *SQLiteIndex) ByStatus(status model.WorkStatus) ([]model.WorkItem, error) {
	return s.scanWork(` WHERE status = ? ORDER BY id`, string(status))
}

func (s *SQLiteIndex) ByType(t model.WorkItemType) ([]model.WorkItem, error) {
	return s.scanWork(` WHERE type = ? ORDER BY id`, string(t))
}

func (s *SQLiteIndex) ActiveByActor(actor string) ([]model.WorkItem, error) {
	return s.scanWork(` WHERE assignee = ? AND status IN ('doing','testing','blocked') ORDER BY id`, actor)
}

func (s *SQLiteIndex) BlockersOf(id string) ([]index.Blocker, error) {
	rows, err := s.db.Query(`
		SELECT d.dep_id,
		       CASE WHEN w.id IS NULL THEN 'missing'
		            WHEN w.status NOT IN ('done','cancelled') THEN 'not-done'
		            ELSE 'done' END
		FROM work_deps d
		LEFT JOIN work_items w ON w.id = d.dep_id
		WHERE d.work_id = ?
		ORDER BY d.dep_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []index.Blocker
	for rows.Next() {
		var b index.Blocker
		b.WorkID = id
		if err := rows.Scan(&b.DepID, &b.Reason); err != nil {
			return nil, err
		}
		if b.Reason != "done" {
			out = append(out, b)
		}
	}
	return out, rows.Err()
}

// --- Registry 查询 ---

func (s *SQLiteIndex) Actor(id string) (model.ActorFile, bool, error) {
	var payload string
	err := s.db.QueryRow(`SELECT payload FROM actors WHERE id = ?`, id).Scan(&payload)
	if err == sql.ErrNoRows {
		return model.ActorFile{}, false, nil
	}
	if err != nil {
		return model.ActorFile{}, false, err
	}
	var af model.ActorFile
	if err := json.Unmarshal([]byte(payload), &af); err != nil {
		return model.ActorFile{}, false, err
	}
	return af, true, nil
}

func (s *SQLiteIndex) Actors() ([]model.ActorFile, error) {
	rows, err := s.db.Query(`SELECT payload FROM actors ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ActorFile
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var af model.ActorFile
		if err := json.Unmarshal([]byte(payload), &af); err != nil {
			return nil, err
		}
		out = append(out, af)
	}
	return out, rows.Err()
}

func (s *SQLiteIndex) System(id string) (model.System, bool, error) {
	var payload string
	err := s.db.QueryRow(`SELECT payload FROM systems WHERE id = ?`, id).Scan(&payload)
	if err == sql.ErrNoRows {
		return model.System{}, false, nil
	}
	if err != nil {
		return model.System{}, false, err
	}
	var sys model.System
	if err := json.Unmarshal([]byte(payload), &sys); err != nil {
		return model.System{}, false, err
	}
	return sys, true, nil
}

func (s *SQLiteIndex) Systems() ([]model.System, error) {
	rows, err := s.db.Query(`SELECT payload FROM systems ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.System
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var sys model.System
		if err := json.Unmarshal([]byte(payload), &sys); err != nil {
			return nil, err
		}
		out = append(out, sys)
	}
	return out, rows.Err()
}

func (s *SQLiteIndex) AssignmentsByActor(actor string) ([]model.Assignment, error) {
	return s.scanAssignments(` WHERE actor = ?`, actor)
}

func (s *SQLiteIndex) AssignmentsBySystem(system string) ([]model.Assignment, error) {
	return s.scanAssignments(` WHERE system = ?`, system)
}

func (s *SQLiteIndex) scanAssignments(where string, args ...any) ([]model.Assignment, error) {
	rows, err := s.db.Query(`SELECT payload FROM assignments`+where+` ORDER BY actor, system, role`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Assignment
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var a model.Assignment
		if err := json.Unmarshal([]byte(payload), &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *SQLiteIndex) Roles() ([]model.Role, error) {
	rows, err := s.db.Query(`SELECT payload FROM roles ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Role
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var r model.Role
		if err := json.Unmarshal([]byte(payload), &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RebuiltAt 返回最近重建时间（index status 用）。
func (s *SQLiteIndex) RebuiltAt() (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = 'rebuilt_at'`).Scan(&v)
	return v, err
}
