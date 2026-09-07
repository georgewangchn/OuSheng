package main

import (
	"os"
	"path/filepath"
	"testing"
)

// seedV1Cards 在 dir 放一张 v1 卡（cards/auth-api.yaml），模拟待迁移的 v1 board。
func seedV1Cards(t *testing.T, dir string) error {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "cards"), 0o755); err != nil {
		return err
	}
	raw := `id: auth-api
owner: backend
task: auth API 上线
status: live
version: 3
contract:
  kind: http
  breaking: false
  interface:
    path: /auth
`
	return os.WriteFile(filepath.Join(dir, "cards", "auth-api.yaml"), []byte(raw), 0o644)
}
