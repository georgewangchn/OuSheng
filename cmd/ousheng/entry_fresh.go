package main

// entry_fresh.go —— 入口新鲜度自检的覆盖面（档案 §58 性质 P2「覆盖由构造得出」）。
//
// 实锤（§56 F4 类）：§55 的入口自检体系盖了 plan / ignite / sync / doctor / web chip，
// 却漏了晨检仪式真正跑的 `cruise status` / `report`——机制在，入口清单是人抄的。
//
// 解法不是把清单抄得更全（下一轮还会漏），是让覆盖**由构造得出**：
//   - 默认接：每个命令都经 `fs.Dir()` 取工作区——首次调用即做一次零网络的确定性
//     板龄自检（落后上游：读数是旧板、此刻写入会与远端错开）并向 stderr 报一句。
//     新命令自动获得，不需要记得。
//   - 例外显式：自带更强自检、或 `--dir` 语义不同的入口，在下表登记并写理由。
//   - 审计派生：TestEntryFreshnessCoverage 从派发器派生入口全集，逐一核对
//     「走了默认（且钩子可达）」或「在表里登记过」；表里的死条目同样报红。
//
// 只报确定性信号（上次 fetch 已知落后）；「久未 fetch」是软信号，留在晨检入口
//（cruise status/report）——高频命令上狼来了会把真信号淹掉（铁律 F2）。

var entryFreshnessExempt = map[string]string{
	"version":       "不读工作区",
	"init":          "创建工作区，尚无上游",
	"setup":         "init 的向导形态",
	"join":          "打印上绳协议，不读板状态",
	"me":            "写机器本地身份文件（gitignored），不是板状态",
	"repo":          "写 repos.yaml（gitignored 本机映射），不是板状态",
	"adapter":       "--dir 指代码仓而非板仓；工作区只验存在",
	"doctor":        "自带全检（含多 remote 水位）",
	"sync":          "自带水位摘要行；pull 即消除落后",
	"cruise plan":   "自带三联门（分叉/分支/多 remote 水位）——驱动器 spawn 闸",
	"cruise ignite": "自带前置门",
	"cruise status": "stdout + JSON 自带板龄（含软信号）",
	"cruise report": "stdout 自带板龄（cron 重定向不丢 stderr）",
	"web":           "常驻服务；页头 chip 每次刷新自检",
}

// entryFreshMsg：公共前置的板龄提示。措辞避开「分叉」二字——驱动器以该词识别
// 等待模式（§48），高频入口的提示不能误触。
func entryFreshMsg(behind string) string {
	return "本地板落后上游 " + behind + " 提交——读数是旧板；此刻写入会与远端错开（推送被拒、需人工合并）：先 ousheng sync"
}
