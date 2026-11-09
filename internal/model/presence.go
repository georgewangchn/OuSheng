package model

import "time"

// Presence 是 PM 在场度（档案 §34：巡航/普通二模式坍缩为一个连续变量——
// 决策响应预期）。present=门可期待分钟级响应；away=挂起转下一活不空等。
// 缺文件/坏文件 = away（悲观默认——无人值守的正确终局是可见的等待）。
// 一行事实，不是权力：只影响 agent 遇门时的等待策略，不改变任何门本身。
type Presence struct {
	Actor string // 声明者（human）
	State string // present | away
	Since string // RFC3339
}

// PresenceStates 封闭枚举。
const (
	PresencePresent = "present"
	PresenceAway    = "away"
)

// PresenceSinceShort 供展示（MM-dd HH:MM）。
func PresenceSinceShort(p Presence) string {
	if t, err := time.Parse(time.RFC3339, p.Since); err == nil {
		return t.Format("01-02 15:04")
	}
	return p.Since
}
