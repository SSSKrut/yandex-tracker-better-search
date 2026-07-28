package server

import (
	"strings"
	"testing"
	"time"

	"github.com/SSSKrut/yandex-tracker-better-search/internal/indexer"

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

func TestTemplateFuncs_Sub100(t *testing.T) {
	if got := templateFunc(t, "{{sub100 .}}", 30); got != "70" {
		t.Errorf("sub100(30) = %q, want \"70\"", got)
	}
}

func TestHighlightHTML_MarkersBecomeTags(t *testing.T) {
	in := indexer.HighlightOpen + "кнопка" + indexer.HighlightClose + " не работает"
	if got := string(highlightHTML(in)); got != "<b>кнопка</b> не работает" {
		t.Errorf("highlightHTML = %q, want the markers rendered as <b>", got)
	}
}

func TestHighlightHTML_EscapesContent(t *testing.T) {
	// Текст вокруг подсветки — это содержимое задачи из трекера. Ни один payload
	// не должен доехать до браузера тегом; единственные теги в выводе — наши <b>.
	payloads := []string{
		`<script>alert(1)</script>`,
		`<img src=x onerror=alert(1)>`,
		`<svg/onload=alert(1)>`,
		`" onmouseover="alert(1)`,
		`<img src="x>" onerror=alert(1)>`,
		`<<script>script>alert(1)<</script>/script>`,
	}

	for _, payload := range payloads {
		got := string(highlightHTML(indexer.HighlightOpen + "гвоздь" + indexer.HighlightClose + " " + payload))

		rest := strings.TrimPrefix(got, "<b>гвоздь</b> ")
		if strings.ContainsAny(rest, "<>") {
			t.Errorf("payload %q leaked markup: %q", payload, got)
		}
		for _, bad := range []string{"onerror", "onload", "onmouseover"} {
			// Атрибут может остаться текстом, но только вне тега — проверяем именно это.
			if strings.Contains(rest, bad) && strings.Contains(rest, "<") {
				t.Errorf("payload %q produced an attribute inside a tag: %q", payload, got)
			}
		}
	}
}

func TestHighlightHTML_ForgedMarkerCannotInjectTags(t *testing.T) {
	// Даже если контент подделает маркер, худшее, что он получит, — жирный текст.
	got := string(highlightHTML(`<b>жирный</b> и <script>alert(1)</script>`))
	if strings.Contains(got, "<script") {
		t.Errorf("content-supplied tags must not survive: %q", got)
	}
	if got != "&lt;b&gt;жирный&lt;/b&gt; и &lt;script&gt;alert(1)&lt;/script&gt;" {
		t.Errorf("unexpected escaping: %q", got)
	}
}
