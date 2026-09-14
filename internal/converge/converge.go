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
	Warnings []string // 不一致但不阻塞收敛（如 done 项进度未满）——保留信号分辨力
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

	var blockers, warnings []string
	hasOpen := false

	actorByID := map[string]model.Actor{}
	if afs, err := idx.Actors(); err == nil {
		for _, af := range afs {
			actorByID[af.Actor.ID] = af.Actor
		}
	}

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
			// 依赖被取消 = 永不满足（比悬空更糟：missing 还可能重现，cancelled 不会）。
			// isDone 把 cancelled 当满足是给「已关闭项的依赖是历史残留」用的语义，
			// 对 open 项的依赖目标必须反着算。
			for _, dep := range w.DependsOn {
				if d, ok, err := idx.Get(dep); err == nil && ok && d.Status == model.StatusCancelled {
					blockers = append(blockers, fmt.Sprintf("%s: dependency %s cancelled (unsatisfiable)", w.ID, dep))
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
		// C2 语义审计：ack 者必须是 human（防手改绕过 + agent 自 ack）
		if w.Contract != nil && w.Contract.Breaking && w.HumanAck != nil && w.HumanAck.Approver != "" {
			if a, ok := actorByID[w.HumanAck.Approver]; ok && a.Type != model.ActorHuman {
				blockers = append(blockers, fmt.Sprintf("%s: human_ack by non-human actor %q", w.ID, w.HumanAck.Approver))
			}
		}

		// 不一致警告：done 项最后上报进度 < 1.0（claimed 与 reported 矛盾，§19）。
		// 无上报的 done 不算矛盾（progress 是可选的诚实汇报）。
		// 只警告不阻塞：若计入 Blockers 会重蹈"waiting 算 BLOCKED"的覆辙，
		// 让收敛信号失去分辨力。
		if w.Status == model.StatusDone && w.Progress != nil && w.Progress.Value < 1.0 {
			warnings = append(warnings, fmt.Sprintf("%s: done but progress reported %.0f%%", w.ID, w.Progress.Value*100))
		}

		// 不一致警告：done 项零证据。基石 ASR 意图的回收（防"过早喊 done 污染下游"）——
		// 终极防线是 accountable human 拍板，这里只让漏网可见，不阻塞。
		if w.Status == model.StatusDone && len(w.Evidence) == 0 {
			warnings = append(warnings, fmt.Sprintf("%s: done without evidence", w.ID))
		}

		// 跨状态机漂移警告：工作已关闭但契约仍 proposed——
		// done = 实施跑到了共识前面；cancelled = 提案悬空未清理。
		// 只盯 proposed：agreed/live 与关闭态并存是合法终态
		// （契约生命周期长于工作，work done + contract live 是接口交付后的常态）。
		if (w.Status == model.StatusDone || w.Status == model.StatusCancelled) &&
			w.Contract != nil && w.Contract.Status == model.ContractProposed {
			warnings = append(warnings, fmt.Sprintf("%s: contract still proposed on closed work", w.ID))
		}
	}

	if len(blockers) > 0 {
		return Result{Status: Blocked, Blockers: blockers, Warnings: warnings}, nil
	}
	if hasOpen {
		return Result{Status: InProgress, Warnings: warnings}, nil
	}
	return Result{Status: Converged, Warnings: warnings}, nil
}
