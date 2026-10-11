package github

import (
	"strings"
	"testing"
)

func TestPushRender(t *testing.T) {
	st := pushStats{
		commits: []string{"a1b2c3d Добавить кэш", "d4e5f6a Тесты"},
		total:   12,
		files:   []fileStat{{"cache.go", 30, 5, "added"}, {"main.go", 10, 2, "modified"}},
		add:     40, del: 7,
	}
	title, body := st.render("main")
	if title != "Push в main: 12 коммитов · 2 файла · +40 −7" {
		t.Fatalf("title = %q", title)
	}
	for _, want := range []string{"• a1b2c3d Добавить кэш", "…и ещё 10", "+30 −5  cache.go (новый)"} {
		if !strings.Contains(body, want) {
			t.Errorf("в body нет %q:\n%s", want, body)
		}
	}
	if title, body := (pushStats{unknownErr: true}).render("dev"); title != "Push в dev" || body != "" {
		t.Fatalf("без статистики: %q %q", title, body)
	}
}
