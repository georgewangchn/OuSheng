package main

import (
	"context"
	"testing"

	"ousheng/internal/model"
	"ousheng/internal/testfix"
)

func TestMCPGetMyContext(t *testing.T) {
	dir := testfix.Setup(t)
	res, out, err := getMyContext(context.Background(), nil, CtxInput{Path: dir, Actor: "backend-agent"})
	if err != nil {
		t.Fatal(err)
	}
	if res != nil {
		t.Fatal("unexpected tool result")
	}
	if out.Actor != "backend-agent" || out.ResponsibleHuman != "zhangsan" {
		t.Fatalf("context wrong: %+v", out)
	}
	if len(out.ActiveWork) != 2 || len(out.Blockers) != 1 || out.Blockers[0] != "K8S-003" {
		t.Fatalf("active/blockers wrong: %+v", out)
	}
}

func TestMCPGetSystemContext(t *testing.T) {
	dir := testfix.Setup(t)
	_, out, err := getSystemContext(context.Background(), nil, CtxSystemInput{Path: dir, System: "datax-backend"})
	if err != nil {
		t.Fatal(err)
	}
	if out.OpenBugs != 1 || len(out.ActiveWork) != 2 {
		t.Fatalf("system view wrong: %+v", out)
	}
}

func TestMCPWorkLifecycle(t *testing.T) {
	dir := testfix.Setup(t)

	// create
	_, created, err := createWorkItem(context.Background(), nil, CreateWorkInput{
		Path: dir, ID: "FEAT-200", Type: "feature", Title: "MCP 冒烟",
		System: "datax-backend", TargetVersion: "v2.1",
		Assignee: "backend-agent", ActingRole: "backend", AccountableHuman: "zhangsan",
		Actor: "zhangsan",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Revision != 1 || created.Status != "backlog" {
		t.Fatalf("create wrong: %+v", created)
	}

	// query
	_, listed, err := queryWorkItems(context.Background(), nil, QueryWorkInput{Path: dir, System: "datax-backend", Open: true})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range listed.Work {
		if w.ID == "FEAT-200" {
			found = true
		}
	}
	if !found {
		t.Fatalf("query missing FEAT-200: %+v", listed.Work)
	}

	// update: backlog → ready → doing
	_, u1, err := updateWorkItem(context.Background(), nil, UpdateWorkInput{Path: dir, ID: "FEAT-200", Status: "ready", Expect: 1})
	if err != nil {
		t.Fatal(err)
	}
	if u1.Revision != 2 {
		t.Fatalf("update rev wrong: %+v", u1)
	}
	if _, _, err := updateWorkItem(context.Background(), nil, UpdateWorkInput{Path: dir, ID: "FEAT-200", Status: "doing", Expect: 1}); err == nil {
		t.Fatal("stale expect must conflict")
	}
	_, u2, err := updateWorkItem(context.Background(), nil, UpdateWorkInput{Path: dir, ID: "FEAT-200", Status: "doing", Expect: 2})
	if err != nil {
		t.Fatal(err)
	}
	_ = u2

	// progress
	_, p, err := reportProgress(context.Background(), nil, ProgressInput{
		Path: dir, ID: "FEAT-200", Value: 0.4, Actor: "backend-agent", Basis: "subtasks",
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Revision != 4 {
		t.Fatalf("progress rev wrong: %+v", p)
	}

	// evidence (manual_check 无需外部验证)
	_, e, err := addEvidence(context.Background(), nil, AddEvidenceInput{
		Path: dir, WorkID: "FEAT-200", Type: "manual_check", Locator: "checklist-v2", Result: "passed",
		Actor: "backend-agent",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = e

	// get_work_item + get_evidence
	_, detail, err := getWorkItem(context.Background(), nil, WorkIDInput{Path: dir, ID: "FEAT-200"})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Progress == nil || detail.Progress.Value != 0.4 {
		t.Fatalf("detail progress wrong: %+v", detail.Progress)
	}
	_, evs, err := getEvidence(context.Background(), nil, WorkIDInput{Path: dir, ID: "FEAT-200"})
	if err != nil {
		t.Fatal(err)
	}
	if len(evs.Evidence) != 1 || evs.Evidence[0].Type != model.EvidenceManualCheck {
		t.Fatalf("evidence wrong: %+v", evs)
	}

	// report bug
	_, bug, err := reportBug(context.Background(), nil, CreateWorkInput{
		Path: dir, ID: "BUG-201", Title: "MCP 报 bug", System: "datax-backend",
		DetectedBy: "test-agent", Actor: "test-agent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if bug.Status != "backlog" {
		t.Fatalf("bug wrong: %+v", bug)
	}

	// assign
	_, a, err := assignWorkItem(context.Background(), nil, AssignWorkInput{
		Path: dir, ID: "BUG-201", Assignee: "backend-agent", ActingRole: "backend",
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.Revision != 2 {
		t.Fatalf("assign rev wrong: %+v", a)
	}

	// actor context（human 视角）
	_, av, err := getActorContext(context.Background(), nil, CtxInput{Path: dir, Actor: "zhangsan"})
	if err != nil {
		t.Fatal(err)
	}
	if len(av.Agents) != 1 || av.Agents[0] != "backend-agent" {
		t.Fatalf("actor view wrong: %+v", av)
	}
}
