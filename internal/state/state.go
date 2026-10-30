// Package state 定义工程状态的持久化抽象（v0.3 §32）。
//
// 第一阶段唯一实现是 gityaml（Git + YAML canonical store）。
// 业务逻辑不得绑死在 Git 上：未来可新增 SQLiteRepository / RemoteRepository，
// 语义模型与业务规则无需重写。
package state

import (
	"errors"

	"ousheng/internal/model"
)

var (
	// ErrNotFound 表示引用的对象不存在。
	ErrNotFound = errors.New("not found")
	// ErrConflict 是 CAS 冲突：expect_revision 与当前 revision 不符。
	ErrConflict = errors.New("revision conflict")
	// ErrExists 表示创建时对象已存在。
	ErrExists = errors.New("already exists")
)

// Repository 是 canonical engineering state 的访问接口。
// 所有写操作在 Core 层做 CAS；Git 只负责持久化、历史、同步。
type Repository interface {
	// InitWorkspace 创建 .ousheng/ 目录骨架、cache gitignore，并初始化 git 仓库（若缺）。
	InitWorkspace(project model.Project) error

	GetProject() (model.Project, error)

	// WorkItem CRUD。Create/Update 携带随本次变更一起提交的 Activity 记录，
	// 保证一次 git commit 原子包含状态变更与审计记录。
	GetWorkItem(id string) (model.WorkItem, error)
	ListWorkItems() ([]model.WorkItem, error)
	CreateWorkItem(w model.WorkItem, acts []model.Activity, msg string) (model.WorkItem, error)
	UpdateWorkItem(w model.WorkItem, expectRevision int, acts []model.Activity, msg string) (model.WorkItem, error)
	// ImportWorkItem 迁移路径：以给定 revision 原样导入（仅限不存在的 id）。
	ImportWorkItem(w model.WorkItem, acts []model.Activity, msg string) (model.WorkItem, error)

	// Registries（读为主；onboarding 写命令经 Save* 落同一 YAML + git 提交）。
	ListActors() ([]model.ActorFile, error)
	GetActor(id string) (model.ActorFile, error)
	ListSystems() ([]model.System, error)
	GetSystem(id string) (model.System, error)
	ListAssignments() ([]model.Assignment, error)
	ListRoles() ([]model.Role, error)
	// Save* 仅用于 onboarding 命令（me/system add/todo 隐式 assignment），
	// 落盘仍是 canonical YAML + git commit，不引入旁路状态。
	SaveActor(f model.ActorFile, msg string) error
	SaveSystems(list []model.System, msg string) error
	SaveRoles(list []model.Role, msg string) error
	SaveAssignments(list []model.Assignment, msg string) error

	// Activity（append-only audit；按时间文件分桶 YYYY-MM.jsonl）。
	AppendActivity(acts []model.Activity, msg string) error
	ListActivity() ([]model.Activity, error)

	// 共识层（v0.4）：designs/ 与 architecture/。
	// 读 = 目录扫描 + frontmatter strict decode（损坏响亮报错）；
	// 写 = 仅生命周期（decide/supersede），内容生成不经绳（牛的活）。
	ListDesigns() ([]model.DesignInfo, error)
	ListArchitecture() ([]string, error)
	GetDesignRaw(topic string) (model.DesignDoc, []byte, error)
	UpdateDesign(topic string, d model.DesignDoc, body []byte, acts []model.Activity, msg string) error
}
