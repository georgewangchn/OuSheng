// Package converge 实现 WorkItem 级收敛检查（v0.3 §40）。
//
// 计算逻辑保持可解释，不引入评分模型：
//
//	所有 required WorkItem done（无 open 项）
//	+ 不存在 unresolved blocker（依赖缺失 / 依赖未完成 / 显式 blocked）
//	+ 必要 evidence 存在（contract verified 无 evidence → BLOCKED）
//	+ 必要 human_ack 存在（breaking 无 ack → BLOCKED）
//	→ CONVERGED；有 open → IN_PROGRESS；有未解除阻塞 → BLOCKED。
package converge

import (
	"fmt"

	"ousheng/internal/graph"
	"ousheng/internal/index"
	"ousheng/internal/model"
)

type Status string

const (
	Converged  Status = "CONVERGED"
	InProgress Status = "IN_PROGRESS"
	Blocked    Status = "BLOCKED"
)

type Result struct {
	Status   Status
	Blockers []string // 未解除阻塞 / 违反 C1/C2 的说明
	Cycle    []string // 依赖环（若有）
}

// Check 只依赖索引：memory 与 sqlite 实现结果必须一致（S5）。
func Check(idx index.Index) (Result, error) {
	all, err := idx.All()
	if err != nil {
		return Result{}, err
	}
	// 1. 依赖环——只看 open 项发出的边。
	// 已关闭项（done/cancelled）的依赖是历史残留，不应永久阻塞收敛
	// （与"done 项的 stale dep 不阻塞"同一语义）。
	deps := map[string][]string{}
	for _, w := range all {
		if model.WorkItemOpen(w.Status) {
			deps[w.ID] = w.DependsOn
		}
	}
	if cyc := graph.FindCycle(deps); cyc != nil {
		return Result{Status: Blocked, Cycle: cyc, Blockers: []string{"dependency cycle: " + fmt.Sprint(cyc)}}, nil
	}

	var blockers []string
	hasOpen := false

	for _, w := range all {
		open := model.WorkItemOpen(w.Status)
		if open {
			hasOpen = true
		}

		// 显式 blocked
		if w.Status == model.StatusBlocked {
			blockers = append(blockers, fmt.Sprintf("%s: blocked", w.ID))
		}

		// 未解除依赖：只算硬阻塞（悬空）。依赖存在且未完成 = 正常依赖链，
		// 属于 IN_PROGRESS 的常态，不是 BLOCKED（反事实：若 waiting 算 BLOCKED，
		// 任何有依赖的并行工作都永远 BLOCKED，收敛信号失去分辨力）。
		if open {
			bs, err := idx.BlockersOf(w.ID)
			if err != nil {
				return Result{}, err
			}
			for _, b := range bs {
				if b.Reason == "missing" {
					blockers = append(blockers, fmt.Sprintf("%s: dangling dependency %s", w.ID, b.DepID))
				}
			}
		}

		// C1：verified 契约必须有 evidence（写路径已挡，此处防手工编辑绕过）
		if w.Contract != nil && w.Contract.Status == model.ContractVerified && len(w.Evidence) == 0 {
			blockers = append(blockers, fmt.Sprintf("%s: contract verified without evidence", w.ID))
		}
		// C2：breaking 必须有 human_ack
		if w.Contract != nil && w.Contract.Breaking && w.HumanAck == nil {
			blockers = append(blockers, fmt.Sprintf("%s: breaking contract without human_ack", w.ID))
		}
	}

	if len(blockers) > 0 {
		return Result{Status: Blocked, Blockers: blockers}, nil
	}
	if hasOpen {
		return Result{Status: InProgress}, nil
	}
	return Result{Status: Converged}, nil
}
