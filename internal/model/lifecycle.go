package model

// CanWorkTransition 报告 WorkItem 执行生命周期迁移合法性（v0.3 §13）。
//
// backlog → ready, cancelled
// ready   → doing, cancelled
// doing   → blocked, testing, done, cancelled
// blocked → doing, cancelled
// testing → doing, done, cancelled
// done    → doing (reopen)
// cancelled → backlog (reopen)
//
// 同态恒真（幂等写入）。
var workEdges = map[WorkStatus][]WorkStatus{
	StatusBacklog:   {StatusReady, StatusCancelled},
	StatusReady:     {StatusDoing, StatusCancelled},
	StatusDoing:     {StatusBlocked, StatusTesting, StatusDone, StatusCancelled},
	StatusBlocked:   {StatusDoing, StatusCancelled},
	StatusTesting:   {StatusDoing, StatusDone, StatusCancelled},
	StatusDone:      {StatusDoing},
	StatusCancelled: {StatusBacklog},
}

func CanWorkTransition(from, to WorkStatus) bool {
	if from == to {
		return true
	}
	for _, t := range workEdges[from] {
		if t == to {
			return true
		}
	}
	return false
}
