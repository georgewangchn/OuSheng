package model

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// HandoverMaxBytes 是交接快照单文件硬门（上下文交接方案 §H1 拍板：4KB）。
// 交接=导航态：超限即内容错居——该去 design/work item/architecture。
// 只卡 save 写路径（agent 面）；human 直改文件不拦（卡点不变式，git 审计兜底）。
const HandoverMaxBytes = 4096

// HandoverInfo 是座位级交接快照的指针（.ousheng/handover/<system>/<actor>.md）。
// 只进 index.Snapshot 供注入面取指针，不进 Index 查询面（S5 同判）。
type HandoverInfo struct {
	System  string
	Actor   string
	SavedAt string // RFC3339
	Session string // 本代 opencode session id（空=换代或未记录）——代际制指针
	Size    int64
}

// HandoverFile 是快照文件的 frontmatter（宽松解码：读面 best-effort，
// 损坏条目跳过不炸——上下文交接方案 §H1 降级路径）。
type HandoverFile struct {
	System  string `yaml:"system"`
	Actor   string `yaml:"actor"`
	SavedAt string `yaml:"saved_at"`
	// Session 是本代 opencode session id（会话代际制，档案 §34）：
	//   非空 = 下一轮可续同代（opencode run -s <id>）——延续性
	//   空   = 换代信号（save 时未带）——上下文精简的重起点
	// 由 save --session 或 handover attach（驱动器补记）写入。
	Session string `yaml:"session,omitempty"`
}

// EncodeHandover 组装快照文件（frontmatter + 正文原样）。
func EncodeHandover(system, actor, savedAt, session, body string) []byte {
	sess := ""
	if session != "" {
		sess = "session: " + session + "\n"
	}
	fm := fmt.Sprintf("---\nsystem: %s\nactor: %s\nsaved_at: %s\n%s---\n", system, actor, savedAt, sess)
	return []byte(fm + body)
}

// DecodeHandover 解析快照文件。损坏返回错误（调用方决定跳过或拒绝）。
func DecodeHandover(raw []byte) (HandoverFile, string, error) {
	s := string(raw)
	if !strings.HasPrefix(s, "---\n") {
		return HandoverFile{}, "", fmt.Errorf("handover must start with frontmatter")
	}
	end := strings.Index(s[4:], "\n---")
	if end < 0 {
		return HandoverFile{}, "", fmt.Errorf("handover frontmatter not closed")
	}
	var f HandoverFile
	if err := yaml.Unmarshal([]byte(s[4:4+end]), &f); err != nil {
		return HandoverFile{}, "", err
	}
	if strings.TrimSpace(f.System) == "" || strings.TrimSpace(f.Actor) == "" {
		return HandoverFile{}, "", fmt.Errorf("handover frontmatter requires system+actor")
	}
	return f, s[4+end+5:], nil
}

// --- credentials 登记表（上下文交接方案 §H2）---

// LocationKind 是凭据值的存放位置类型。登记表只做位置索引（值永不入 git），
// 永不做配置分发——防配置中心死法复辟。
type LocationKind string

const (
	LocLocalFile       LocationKind = "local-file"       // .ousheng/secrets/<system>.env
	LocPasswordManager LocationKind = "password-manager" // 密码管理器条目（生产值唯一合法位置）
	LocEnvVar          LocationKind = "env-var"          // 进程环境变量名
)

// Credential 是一条凭据登记（位置入册，P7 产品化）。
type Credential struct {
	ID       string             `yaml:"id"`
	System   string             `yaml:"system"`
	Env      string             `yaml:"env,omitempty"` // dev/uat/gray/canary/show/pro（开放集）
	Desc     string             `yaml:"desc,omitempty"`
	Location CredentialLocation `yaml:"location"`
}

// CredentialLocation 描述值在哪（kind+ref）。
type CredentialLocation struct {
	Kind LocationKind `yaml:"kind"`
	Ref  string       `yaml:"ref,omitempty"` // local-file: secrets/data.env#KEY；password-manager: 条目名；env-var: VAR 名
}

// CredentialsFile 是 .ousheng/credentials.yaml（手改 + git 审计；CLI 写面判据延后）。
type CredentialsFile struct {
	SchemaVersion int          `yaml:"schema_version"`
	Credentials   []Credential `yaml:"credentials"`
}

// DecodeCredentials 严格解码（校验手写纪律：未知键拒绝、kind 枚举、id 非空）。
func DecodeCredentials(raw []byte) (CredentialsFile, error) {
	var f CredentialsFile
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return f, err
	}
	seen := map[string]bool{}
	for _, c := range f.Credentials {
		if strings.TrimSpace(c.ID) == "" {
			return f, fmt.Errorf("credential id is required")
		}
		if seen[c.ID] {
			return f, fmt.Errorf("duplicate credential id %q", c.ID)
		}
		seen[c.ID] = true
		switch c.Location.Kind {
		case LocLocalFile, LocPasswordManager, LocEnvVar:
		case "":
			return f, fmt.Errorf("credential %s: location.kind is required", c.ID)
		default:
			return f, fmt.Errorf("credential %s: invalid location.kind %q", c.ID, c.Location.Kind)
		}
	}
	return f, nil
}
