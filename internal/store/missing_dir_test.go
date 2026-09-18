package store

import "testing"

// 空目录不进 git：clone 后 cards/ 缺失 = 空看板，不是错误。
func TestListMissingCardsDir(t *testing.T) {
	s := Open(t.TempDir())
	cards, err := s.List()
	if err != nil {
		t.Fatalf("cards/ 缺失不应报错: %v", err)
	}
	if len(cards) != 0 {
		t.Fatalf("应为空看板，得到 %d 张", len(cards))
	}
}
