package govern

// ledger.go —— 裁决账本：解析 + 完整性约束（档案 §57）。
//
// 判据（四连复发的纯文字裁决）：§51 绳牛四代、§50-G presence 两轮、§50-A 单写源
// 8 天 11 次 merge、「四剧本必跑」红 8 天。共同点：裁决只有文字，没有执行连接。
// 本校验器把「裁决 → 机制」的翻译步从「靠记得」变成「不填就红、到期就红」。
// 它自己只经 TestRulingLedger 运行（无运行时消费者，不进任何二进制语义）。

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ledgerFrom：账本起始节号（§56 起；此前 55 节是历史，不追溯改写）。
const ledgerFrom = 56

// enforcementTypes：执行类型封闭枚举（词汇层，见 doc.go）。
var enforcementTypes = map[string]bool{
	"gate": true, "default": true, "meter": true, "lock": true, "hook": true, "discipline": true,
}

// disciplineMaxDays：纪律类复审日的上限——不许一脚踢到远将来绕过复审。
const disciplineMaxDays = 90

type rulingHeader struct {
	Section     int
	Enforcement []string
	Locks       []string
	Hook        []string
	Meter       string
	Review      string
}

var (
	sectionRe = regexp.MustCompile(`(?m)^## (\d+)\. (.*)$`)
	rulingRe  = regexp.MustCompile(`(?s)<!-- ruling:\n(.*?)-->`)
	fieldRe   = regexp.MustCompile(`(?m)^\s*([a-z]+):\s*(.*?)\s*$`)
	testFnRe  = regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)
)

// parseLedger 抽取档案里 ≥ ledgerFrom 的每个二级节及其 ruling 头。
// 第二个返回值：缺头的节号。
func parseLedger(archive string) ([]rulingHeader, []int) {
	locs := sectionRe.FindAllStringSubmatchIndex(archive, -1)
	var out []rulingHeader
	var missing []int
	for k, loc := range locs {
		n, err := strconv.Atoi(archive[loc[2]:loc[3]])
		if err != nil || n < ledgerFrom {
			continue
		}
		end := len(archive)
		if k+1 < len(locs) {
			end = locs[k+1][0]
		}
		body := archive[loc[1]:end]
		m := rulingRe.FindStringSubmatch(body)
		if m == nil {
			missing = append(missing, n)
			continue
		}
		h := rulingHeader{Section: n}
		for _, f := range fieldRe.FindAllStringSubmatch(m[1], -1) {
			switch f[1] {
			case "enforcement":
				h.Enforcement = splitList(f[2])
			case "locks":
				h.Locks = splitList(f[2])
			case "hook":
				h.Hook = splitList(f[2])
			case "meter":
				h.Meter = f[2]
			case "review":
				h.Review = f[2]
			}
		}
		out = append(out, h)
	}
	return out, missing
}

func splitList(v string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(v, func(r rune) bool {
		return r == ',' || r == '，' || r == ' ' || r == '\t' || r == ';' || r == '；'
	}) {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// validateHeader 是账本的完整性约束（审计层派生谓词）。返回违规描述，空 = 合规。
//
//	V1 结构：enforcement 非空且取值合法
//	V2 锁存在：lock 类的每个 locks 名字必须是仓里真实的测试函数
//	V3 机制必有锁：gate / default / meter 类必须带 locks
//	V4 钩存在：hook 类的每个路径必须存在
//	V5 纪律限时：discipline 类必须配 meter + review；review 不得晚于今天+90 天；到期即红
//	V6 量尺必名：meter 类必须写明量尺
func validateHeader(h rulingHeader, now time.Time, hasTest func(string) bool, fileExists func(string) bool) []string {
	var errs []string
	has := func(k string) bool {
		for _, e := range h.Enforcement {
			if e == k {
				return true
			}
		}
		return false
	}
	sec := "§" + strconv.Itoa(h.Section)
	if len(h.Enforcement) == 0 {
		errs = append(errs, sec+" ruling 头缺 enforcement（执行类型：gate/default/meter/lock/hook，或 discipline）")
	}
	for _, e := range h.Enforcement {
		if !enforcementTypes[e] {
			errs = append(errs, sec+" enforcement 含未知类型 "+e+"（合法：gate/default/meter/lock/hook/discipline）")
		}
	}
	if has("lock") {
		if len(h.Locks) == 0 {
			errs = append(errs, sec+" 声明 lock 却未列 locks:")
		}
		for _, l := range h.Locks {
			if !hasTest(l) {
				errs = append(errs, sec+" locks: "+l+" 不是仓里的测试函数（写了名字不等于有锁）")
			}
		}
	}
	if (has("gate") || has("default") || has("meter")) && len(h.Locks) == 0 {
		errs = append(errs, sec+" 声明了机制（gate/default/meter）却没有 locks——机制必须有行为锁（铁律 E6）")
	}
	if has("hook") {
		if len(h.Hook) == 0 {
			errs = append(errs, sec+" 声明 hook 却未列 hook: 路径")
		}
		for _, p := range h.Hook {
			if !fileExists(p) {
				errs = append(errs, sec+" hook: "+p+" 不存在")
			}
		}
	}
	if has("meter") && h.Meter == "" {
		errs = append(errs, sec+" 声明 meter 却未写 meter: 量尺（失效时看哪条命令的什么读数）")
	}
	if has("discipline") {
		if h.Meter == "" {
			errs = append(errs, sec+" 纪律类裁决必须配 meter: 量尺——没有量尺的纪律无法证明还活着")
		}
		switch {
		case h.Review == "":
			errs = append(errs, sec+" 纪律类裁决必须带 review: YYYY-MM-DD 复审日（纪律只许作为限时临时态）")
		default:
			rv, err := time.Parse("2006-01-02", h.Review)
			if err != nil {
				errs = append(errs, sec+" review: "+h.Review+" 不是 YYYY-MM-DD")
				break
			}
			if max := now.AddDate(0, 0, disciplineMaxDays); rv.After(max) {
				errs = append(errs, sec+" review: "+h.Review+" 晚于今天 + "+strconv.Itoa(disciplineMaxDays)+" 天（"+max.Format("2006-01-02")+"）——复审日不许一脚踢远绕过复审")
			}
			if now.After(rv.AddDate(0, 0, 1)) {
				errs = append(errs, sec+" 纪律复审到期（"+h.Review+"）——测量 meter（"+h.Meter+"），把读数写回头部 measured: 后续期，或转成 gate/default/meter/hook")
			}
		}
	}
	return errs
}

// indexTestFuncs：仓里全部测试函数名（locks: 的存在性判据）。
func indexTestFuncs(root string) map[string]bool {
	out := map[string]bool{}
	walkGoFiles(root, func(path string, isTest bool, src []byte) {
		if !isTest {
			return
		}
		for _, m := range testFnRe.FindAllSubmatch(src, -1) {
			out[string(m[1])] = true
		}
	})
	return out
}

// skipDirs：非产品目录（板仓 clone / 依赖 / 历史素材 / 本机工具）。
var skipDirs = map[string]bool{
	".git": true, "kanban": true, "node_modules": true, "assets": true,
	".claude": true, ".workbuddy": true, ".omo": true, "内容": true, "比赛材料": true,
}

// walkGoFiles 遍历仓内 .go 文件。
func walkGoFiles(root string, fn func(path string, isTest bool, src []byte)) {
	_ = filepath.Walk(root, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		fn(path, strings.HasSuffix(path, "_test.go"), src)
		return nil
	})
}
