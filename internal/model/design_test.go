package model

import (
	"strings"
	"testing"
)

func designRaw(status string) []byte {
	return []byte(`---
status: ` + status + `
owner: datax-agent
systems: [datax, ui]
related_items: [REQ-DATAX-007]
---
# 方案

正文自由。
`)
}

func TestDecodeDesignDocDraft(t *testing.T) {
	d, body, err := DecodeDesignDoc(designRaw("draft"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != DesignDraft || d.Owner != "datax-agent" {
		t.Fatalf("decode wrong: %+v", d)
	}
	if len(d.Systems) != 2 || d.Systems[0] != "datax" {
		t.Fatalf("systems wrong: %v", d.Systems)
	}
	if !strings.Contains(string(body), "正文自由") {
		t.Fatalf("body lost: %q", body)
	}
}

func TestDecodeDesignDocUnknownKeyRejected(t *testing.T) {
	raw := []byte("---\nstatus: draft\nowner: a\nhax: true\n---\nbody\n")
	if _, _, err := DecodeDesignDoc(raw); err == nil || !strings.Contains(err.Error(), "hax") {
		t.Fatalf("unknown key must be rejected, got %v", err)
	}
}

func TestDecodeDesignDocBadStatusRejected(t *testing.T) {
	if _, _, err := DecodeDesignDoc(designRaw("live")); err == nil {
		t.Fatal("invalid status must be rejected")
	}
	if _, _, err := DecodeDesignDoc([]byte("---\nowner: a\n---\nb\n")); err == nil {
		t.Fatal("missing status must be rejected")
	}
}

func TestDecodeDesignDocAgreedRequiresDecider(t *testing.T) {
	raw := []byte("---\nstatus: agreed\nowner: a\n---\nb\n")
	if _, _, err := DecodeDesignDoc(raw); err == nil || !strings.Contains(err.Error(), "decided_by") {
		t.Fatalf("agreed without decided_by must be rejected, got %v", err)
	}
}

func TestDecodeDesignDocSupersededRequiresSuccessor(t *testing.T) {
	raw := []byte("---\nstatus: superseded\nowner: a\n---\nb\n")
	if _, _, err := DecodeDesignDoc(raw); err == nil || !strings.Contains(err.Error(), "superseded_by") {
		t.Fatalf("superseded without superseded_by must be rejected, got %v", err)
	}
}

func TestDecodeDesignDocOwnerRequired(t *testing.T) {
	raw := []byte("---\nstatus: draft\nowner: \"\"\n---\nb\n")
	if _, _, err := DecodeDesignDoc(raw); err == nil || !strings.Contains(err.Error(), "owner") {
		t.Fatalf("empty owner must be rejected, got %v", err)
	}
}

func TestDecodeDesignDocBadSystemRejected(t *testing.T) {
	raw := []byte("---\nstatus: draft\nowner: a\nsystems: [Bad Sys]\n---\nb\n")
	if _, _, err := DecodeDesignDoc(raw); err == nil {
		t.Fatal("invalid system id must be rejected")
	}
}

func TestDecodeDesignDocNoFrontmatterRejected(t *testing.T) {
	if _, _, err := DecodeDesignDoc([]byte("# just markdown\n")); err == nil {
		t.Fatal("missing frontmatter must be rejected")
	}
}

func TestEncodeDesignDocRoundtrip(t *testing.T) {
	d, body, err := DecodeDesignDoc(designRaw("draft"))
	if err != nil {
		t.Fatal(err)
	}
	d.Status = DesignAgreed
	d.DecidedBy = "pm"
	d.DecidedAt = "2026-09-15T10:00:00+08:00"
	out, err := EncodeDesignDoc(d, body)
	if err != nil {
		t.Fatal(err)
	}
	d2, body2, err := DecodeDesignDoc(out)
	if err != nil {
		t.Fatal(err)
	}
	if d2.DecidedBy != "pm" || d2.Status != DesignAgreed {
		t.Fatalf("roundtrip wrong: %+v", d2)
	}
	if string(body2) != string(body) {
		t.Fatalf("body must be preserved verbatim: %q vs %q", body2, body)
	}
}

func TestParseRoundSpeakers(t *testing.T) {
	raw := []byte(`# Round 1

## datax-agent — 2026-09-15
赞成，条件：先出契约。

## ui-agent — 2026-09-15
有异议：下载地址会变。

正文里 ## 之后的行不该被误认 —— 除非行首。indented ` + "## fake-agent" + `
`)
	got := ParseRoundSpeakers(raw)
	if len(got) != 2 || got[0] != "datax-agent" || got[1] != "ui-agent" {
		t.Fatalf("speakers wrong: %v", got)
	}
	if ParseRoundSpeakers([]byte("no headers")) != nil {
		t.Fatal("no headers must return nil (安全默认全员待发言)")
	}
}
