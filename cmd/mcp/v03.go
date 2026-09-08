package main

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	gitadapter "ousheng/adapters/git"
	octx "ousheng/internal/context"
	"ousheng/internal/model"
	"ousheng/internal/state/gityaml"
	"ousheng/internal/workspace"
)

// 查看时机协议（三时机，低频事件驱动，不做每 loop 轮询）：
// ① 每日/session 启动 → get_my_context
// ② 遇到 bug/问题 → query_work_items / get_system_context
// ③ 任务结束 → update_work_item / report_progress / add_evidence，然后 get_my_context
const timingHint = "Consultation timing: call at session start, when hitting a problem/blocker, or when finishing a task — not every loop."

type CtxInput struct {
	Path  string `json:"path" jsonschema:"workspace root (git repo containing .ousheng/)"`
	Actor string `json:"actor" jsonschema:"actor id, e.g. backend-agent"`
}

type CtxSystemInput struct {
	Path   string `json:"path" jsonschema:"workspace root"`
	System string `json:"system" jsonschema:"system id, e.g. datax-backend"`
}

func newCtxService(path string) (*octx.Service, error) {
	return octx.New(gityaml.Open(path))
}

func getMyContext(_ context.Context, _ *mcp.CallToolRequest, in CtxInput) (*mcp.CallToolResult, octx.MyContext, error) {
	c, err := newCtxService(in.Path)
	if err != nil {
		return nil, octx.MyContext{}, err
	}
	mc, err := c.GetMyContext(in.Actor)
	if err != nil {
		return nil, octx.MyContext{}, err
	}
	return nil, *mc, nil
}

func getActorContext(_ context.Context, _ *mcp.CallToolRequest, in CtxInput) (*mcp.CallToolResult, octx.ActorView, error) {
	c, err := newCtxService(in.Path)
	if err != nil {
		return nil, octx.ActorView{}, err
	}
	v, err := c.GetActorContext(in.Actor)
	if err != nil {
		return nil, octx.ActorView{}, err
	}
	return nil, *v, nil
}

func getSystemContext(_ context.Context, _ *mcp.CallToolRequest, in CtxSystemInput) (*mcp.CallToolResult, octx.SystemView, error) {
	c, err := newCtxService(in.Path)
	if err != nil {
		return nil, octx.SystemView{}, err
	}
	v, err := c.GetSystemContext(in.System)
	if err != nil {
		return nil, octx.SystemView{}, err
	}
	return nil, *v, nil
}

type WorkIDInput struct {
	Path string `json:"path" jsonschema:"workspace root"`
	ID   string `json:"id" jsonschema:"work item id"`
}

func getWorkItem(_ context.Context, _ *mcp.CallToolRequest, in WorkIDInput) (*mcp.CallToolResult, octx.WorkItemDetail, error) {
	c, err := newCtxService(in.Path)
	if err != nil {
		return nil, octx.WorkItemDetail{}, err
	}
	d, err := c.GetWorkItem(in.ID)
	if err != nil {
		return nil, octx.WorkItemDetail{}, err
	}
	return nil, *d, nil
}

func getEvidence(_ context.Context, _ *mcp.CallToolRequest, in WorkIDInput) (*mcp.CallToolResult, EvidenceListOutput, error) {
	c, err := newCtxService(in.Path)
	if err != nil {
		return nil, EvidenceListOutput{}, err
	}
	evs, err := c.GetEvidence(in.ID)
	if err != nil {
		return nil, EvidenceListOutput{}, err
	}
	if evs == nil {
		evs = []model.Evidence{}
	}
	return nil, EvidenceListOutput{Evidence: evs}, nil
}

type EvidenceListOutput struct {
	Evidence []model.Evidence `json:"evidence"`
}

type QueryWorkInput struct {
	Path     string `json:"path" jsonschema:"workspace root"`
	System   string `json:"system,omitempty" jsonschema:"filter by system"`
	Status   string `json:"status,omitempty" jsonschema:"filter by status: backlog|ready|doing|blocked|testing|done|cancelled"`
	Assignee string `json:"assignee,omitempty" jsonschema:"filter by assignee actor"`
	Version  string `json:"version,omitempty" jsonschema:"filter by target_version"`
	Type     string `json:"type,omitempty" jsonschema:"filter by type: requirement|feature|bug|task|test|deployment|release"`
	Open     bool   `json:"open,omitempty" jsonschema:"only open (not done/cancelled)"`
}

type QueryWorkOutput struct {
	Work []model.WorkSummary `json:"work"`
}

func queryWorkItems(_ context.Context, _ *mcp.CallToolRequest, in QueryWorkInput) (*mcp.CallToolResult, QueryWorkOutput, error) {
	c, err := newCtxService(in.Path)
	if err != nil {
		return nil, QueryWorkOutput{}, err
	}
	var items []model.WorkItem
	switch {
	case in.Assignee != "":
		items, err = c.Idx.ByAssignee(in.Assignee)
	case in.System != "":
		items, err = c.Idx.BySystem(in.System)
	case in.Version != "":
		items, err = c.Idx.ByVersion(in.Version)
	default:
		items, err = c.Idx.All()
	}
	if err != nil {
		return nil, QueryWorkOutput{}, err
	}
	out := QueryWorkOutput{Work: []model.WorkSummary{}}
	for _, w := range items {
		if in.Status != "" && string(w.Status) != in.Status {
			continue
		}
		if in.Type != "" && string(w.Type) != in.Type {
			continue
		}
		if in.System != "" && w.System != in.System {
			continue
		}
		if in.Open && !model.WorkItemOpen(w.Status) {
			continue
		}
		out.Work = append(out.Work, w.Summarize())
	}
	return nil, out, nil
}

type CreateWorkInput struct {
	Path             string   `json:"path" jsonschema:"workspace root"`
	ID               string   `json:"id" jsonschema:"work item id (UPPER-xxx style, e.g. BUG-017)"`
	Type             string   `json:"type" jsonschema:"requirement|feature|bug|task|test|deployment|release"`
	Title            string   `json:"title" jsonschema:"one-line title"`
	System           string   `json:"system,omitempty" jsonschema:"system id"`
	TargetVersion    string   `json:"target_version,omitempty" jsonschema:"target product version, e.g. v2.0"`
	Assignee         string   `json:"assignee,omitempty" jsonschema:"assignee actor id"`
	ActingRole       string   `json:"acting_role,omitempty" jsonschema:"acting role id"`
	AccountableHuman string   `json:"accountable_human,omitempty" jsonschema:"accountable human actor id"`
	DetectedBy       string   `json:"detected_by,omitempty" jsonschema:"who detected this (bug)"`
	DependsOn        []string `json:"depends_on,omitempty" jsonschema:"dependency work item ids"`
	Actor            string   `json:"actor,omitempty" jsonschema:"who creates (activity attribution)"`
}

type WorkWriteOutput struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
	Status   string `json:"status"`
}

func createWorkItem(_ context.Context, _ *mcp.CallToolRequest, in CreateWorkInput) (*mcp.CallToolResult, WorkWriteOutput, error) {
	svc := workspace.New(gityaml.Open(in.Path))
	w, err := svc.Create(model.WorkItem{
		SchemaVersion:    2,
		ID:               in.ID,
		Type:             model.WorkItemType(in.Type),
		Title:            in.Title,
		System:           in.System,
		TargetVersion:    in.TargetVersion,
		Assignee:         in.Assignee,
		ActingRole:       in.ActingRole,
		AccountableHuman: in.AccountableHuman,
		DetectedBy:       in.DetectedBy,
		DependsOn:        in.DependsOn,
	}, in.Actor)
	if err != nil {
		return nil, WorkWriteOutput{}, err
	}
	return nil, WorkWriteOutput{ID: w.ID, Revision: w.Revision, Status: string(w.Status)}, nil
}

func reportBug(_ context.Context, _ *mcp.CallToolRequest, in CreateWorkInput) (*mcp.CallToolResult, WorkWriteOutput, error) {
	svc := workspace.New(gityaml.Open(in.Path))
	w, err := svc.Create(model.WorkItem{
		SchemaVersion:    2,
		ID:               in.ID,
		Type:             model.TypeBug,
		Title:            in.Title,
		System:           in.System,
		TargetVersion:    in.TargetVersion,
		Assignee:         in.Assignee,
		ActingRole:       in.ActingRole,
		AccountableHuman: in.AccountableHuman,
		DetectedBy:       in.DetectedBy,
		DependsOn:        in.DependsOn,
	}, in.Actor)
	if err != nil {
		return nil, WorkWriteOutput{}, err
	}
	return nil, WorkWriteOutput{ID: w.ID, Revision: w.Revision, Status: string(w.Status)}, nil
}

type UpdateWorkInput struct {
	Path   string `json:"path" jsonschema:"workspace root"`
	ID     string `json:"id" jsonschema:"work item id"`
	Status string `json:"status,omitempty" jsonschema:"new status: backlog|ready|doing|blocked|testing|done|cancelled"`
	Expect int    `json:"expect" jsonschema:"expected revision (CAS); -1 = use current"`
	Actor  string `json:"actor,omitempty" jsonschema:"who updates (activity attribution)"`
}

func updateWorkItem(_ context.Context, _ *mcp.CallToolRequest, in UpdateWorkInput) (*mcp.CallToolResult, WorkWriteOutput, error) {
	svc := workspace.New(gityaml.Open(in.Path))
	cur, err := svc.Repo.GetWorkItem(in.ID)
	if err != nil {
		return nil, WorkWriteOutput{}, err
	}
	expect := in.Expect
	if expect <= 0 {
		expect = cur.Revision
	}
	if in.Status != "" {
		cur.Status = model.WorkStatus(in.Status)
	}
	w, err := svc.Update(cur, expect, in.Actor)
	if err != nil {
		return nil, WorkWriteOutput{}, err
	}
	return nil, WorkWriteOutput{ID: w.ID, Revision: w.Revision, Status: string(w.Status)}, nil
}

type AssignWorkInput struct {
	Path       string `json:"path" jsonschema:"workspace root"`
	ID         string `json:"id" jsonschema:"work item id"`
	Assignee   string `json:"assignee" jsonschema:"assignee actor id"`
	ActingRole string `json:"acting_role,omitempty" jsonschema:"acting role id"`
	Expect     int    `json:"expect" jsonschema:"expected revision (CAS); -1 = use current"`
	Actor      string `json:"actor,omitempty" jsonschema:"who assigns"`
}

func assignWorkItem(_ context.Context, _ *mcp.CallToolRequest, in AssignWorkInput) (*mcp.CallToolResult, WorkWriteOutput, error) {
	svc := workspace.New(gityaml.Open(in.Path))
	cur, err := svc.Repo.GetWorkItem(in.ID)
	if err != nil {
		return nil, WorkWriteOutput{}, err
	}
	expect := in.Expect
	if expect <= 0 {
		expect = cur.Revision
	}
	w, err := svc.Assign(in.ID, in.Assignee, in.ActingRole, expect, in.Actor)
	if err != nil {
		return nil, WorkWriteOutput{}, err
	}
	return nil, WorkWriteOutput{ID: w.ID, Revision: w.Revision, Status: string(w.Status)}, nil
}

type ProgressInput struct {
	Path   string  `json:"path" jsonschema:"workspace root"`
	ID     string  `json:"id" jsonschema:"work item id"`
	Value  float64 `json:"value" jsonschema:"progress 0..1 (reported, not fact)"`
	Actor  string  `json:"actor" jsonschema:"reporting actor id"`
	Basis  string  `json:"basis,omitempty" jsonschema:"manual|implementation-checklist|test-cases|subtasks|story-points|milestone"`
	Expect int     `json:"expect" jsonschema:"expected revision (CAS); -1 = use current"`
}

func reportProgress(_ context.Context, _ *mcp.CallToolRequest, in ProgressInput) (*mcp.CallToolResult, WorkWriteOutput, error) {
	svc := workspace.New(gityaml.Open(in.Path))
	cur, err := svc.Repo.GetWorkItem(in.ID)
	if err != nil {
		return nil, WorkWriteOutput{}, err
	}
	expect := in.Expect
	if expect <= 0 {
		expect = cur.Revision
	}
	basis := in.Basis
	if basis == "" {
		basis = string(model.BasisManual)
	}
	p := model.ProgressReport{
		Value: in.Value, Actor: in.Actor,
		ReportedAt: time.Now().Format(time.RFC3339), Basis: model.ProgressBasis(basis),
	}
	w, err := svc.ReportProgress(in.ID, p, expect)
	if err != nil {
		return nil, WorkWriteOutput{}, err
	}
	return nil, WorkWriteOutput{ID: w.ID, Revision: w.Revision, Status: string(w.Status)}, nil
}

type AddEvidenceInput struct {
	Path    string `json:"path" jsonschema:"workspace root"`
	WorkID  string `json:"work_id" jsonschema:"work item id"`
	Type    string `json:"type" jsonschema:"git_commit|pull_request|test_result|ci_run|deployment|log|manual_check|document"`
	Locator string `json:"locator" jsonschema:"external locator (commit hash, run id, ...)"`
	Source  string `json:"source,omitempty" jsonschema:"source system, e.g. git, github-actions, pytest"`
	Result  string `json:"result,omitempty" jsonschema:"passed|failed|..."`
	Note    string `json:"note,omitempty" jsonschema:"free note"`
	Expect  int    `json:"expect" jsonschema:"expected revision (CAS); -1 = use current"`
	Actor   string `json:"actor,omitempty" jsonschema:"who adds the evidence"`
}

func addEvidence(_ context.Context, _ *mcp.CallToolRequest, in AddEvidenceInput) (*mcp.CallToolResult, WorkWriteOutput, error) {
	svc := workspace.New(gityaml.Open(in.Path))
	cur, err := svc.Repo.GetWorkItem(in.WorkID)
	if err != nil {
		return nil, WorkWriteOutput{}, err
	}
	expect := in.Expect
	if expect <= 0 {
		expect = cur.Revision
	}
	ev := model.Evidence{
		Type: model.EvidenceType(in.Type), Source: in.Source, Locator: in.Locator,
		Result: in.Result, Note: in.Note, ObservedAt: time.Now().Format(time.RFC3339),
	}
	// git_commit：经 adapter 验证存在性
	if ev.Type == model.EvidenceGitCommit {
		verified, err := gitadapter.EvidenceForCommit(in.Path, in.Locator)
		if err != nil {
			return nil, WorkWriteOutput{}, fmt.Errorf("verify git_commit: %w", err)
		}
		if ev.Note == "" {
			ev.Note = verified.Note
		}
		if ev.Source == "" {
			ev.Source = "git"
		}
	}
	w, err := svc.AddEvidence(in.WorkID, ev, expect, in.Actor)
	if err != nil {
		return nil, WorkWriteOutput{}, err
	}
	return nil, WorkWriteOutput{ID: w.ID, Revision: w.Revision, Status: string(w.Status)}, nil
}
