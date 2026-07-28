package server

import (
	"strings"
	"testing"
	"time"

	syncer "github.com/SSSKrut/yandex-tracker-better-search/internal/sync"
)

func TestFormatDuration(t *testing.T) {
	// Раньше число печаталось как string(rune('0'+i%10)), то есть от него
	// оставалась последняя цифра: "45 минут назад" превращалось в "5 минут назад".
	tests := []struct {
		n    float64
		want string
	}{
		{1, "1 минуту"},
		{2, "2 минуты"},
		{4, "4 минуты"},
		{5, "5 минут"},
		{11, "11 минут"},
		{12, "12 минут"},
		{14, "14 минут"},
		{21, "21 минуту"},
		{22, "22 минуты"},
		{25, "25 минут"},
		{45, "45 минут"},
		{59, "59 минут"},
	}

	for _, tc := range tests {
		if got := formatDuration(tc.n, "минуту", "минуты", "минут"); got != tc.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestFormatDuration_Hours(t *testing.T) {
	tests := map[float64]string{
		1:  "1 час",
		2:  "2 часа",
		5:  "5 часов",
		11: "11 часов",
		21: "21 час",
		23: "23 часа",
	}

	for n, want := range tests {
		if got := formatDuration(n, "час", "часа", "часов"); got != want {
			t.Errorf("formatDuration(%v) = %q, want %q", n, got, want)
		}
	}
}

func TestProgressPercent(t *testing.T) {
	tests := []struct {
		name string
		p    syncer.Progress
		want int
	}{
		{"пусто", syncer.Progress{}, 0},
		{"total неизвестен", syncer.Progress{Current: 10}, 0},
		{"ничего не сделано", syncer.Progress{Total: 10}, 0},
		{"половина", syncer.Progress{Current: 5, Total: 10}, 50},
		{"готово", syncer.Progress{Current: 10, Total: 10}, 100},
		{"перелёт клампится", syncer.Progress{Current: 20, Total: 10}, 100},
		{"отрицательный total", syncer.Progress{Current: 5, Total: -1}, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := progressPercent(tc.p); got != tc.want {
				t.Errorf("progressPercent(%+v) = %d, want %d", tc.p, got, tc.want)
			}
		})
	}
}

func TestProgressStageLabel(t *testing.T) {
	tests := map[string]string{
		syncer.StageIssues:   "Загружаем задачи",
		syncer.StageComments: "Комментарии и файлы",
		syncer.StageIndexing: "Индексация",
		"":                   "Синхронизация",
		"что-то новое":       "Синхронизация",
	}

	for stage, want := range tests {
		if got := progressStageLabel(stage); got != want {
			t.Errorf("progressStageLabel(%q) = %q, want %q", stage, got, want)
		}
	}
}

// templateFunc - вызывает функцию из FuncMap шаблонов через сам шаблон,
// потому что напрямую они не экспортируются.
func templateFunc(t *testing.T, expr string, data any) string {
	t.Helper()

	srv := newTestServer(t, &fakeAPI{})
	tmpl, err := srv.templates.Clone()
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	if _, err := tmpl.New("probe").Parse(expr); err != nil {
		t.Fatalf("parse %q: %v", expr, err)
	}

	var sb strings.Builder
	if err := tmpl.ExecuteTemplate(&sb, "probe", data); err != nil {
		t.Fatalf("execute %q: %v", expr, err)
	}
	return sb.String()
}

func TestTemplateFuncs_ZeroTimeIsNever(t *testing.T) {
	if got := templateFunc(t, "{{formatTime .}}", time.Time{}); got != "никогда" {
		t.Errorf("formatTime(zero) = %q, want \"никогда\"", got)
	}
	if got := templateFunc(t, "{{timeAgo .}}", time.Time{}); got != "никогда" {
		t.Errorf("timeAgo(zero) = %q, want \"никогда\"", got)
	}
}

func TestTemplateFuncs_TimeAgo(t *testing.T) {
	tests := []struct {
		name string
		ago  time.Duration
		want string
	}{
		{"только что", 10 * time.Second, "только что"},
		{"минуты", 45 * time.Minute, "45 минут назад"},
		{"часы", 3 * time.Hour, "3 часа назад"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := templateFunc(t, "{{timeAgo .}}", time.Now().Add(-tc.ago))
			if got != tc.want {
				t.Errorf("timeAgo(-%s) = %q, want %q", tc.ago, got, tc.want)
			}
		})
	}
}

func TestTemplateFuncs_SafeHTMLAndSub100(t *testing.T) {
	// safeHTML нужен для подсветки <b> из HIGHLIGHT() — она не должна экранироваться.
	if got := templateFunc(t, "{{safeHTML .}}", "<b>кнопка</b>"); got != "<b>кнопка</b>" {
		t.Errorf("safeHTML = %q, want unescaped markup", got)
	}
	if got := templateFunc(t, "{{sub100 .}}", 30); got != "70" {
		t.Errorf("sub100(30) = %q, want \"70\"", got)
	}
}
