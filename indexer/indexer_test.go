package indexer

import (
	"strings"
	"testing"
)

func TestEscapeSQL_StripsControlAndEscapes(t *testing.T) {
	in := "  hi\nthere\tit's\\ok\x00\x1f  "
	got := escapeSQL(in)

	if strings.Contains(got, "\x00") {
		t.Fatalf("escapeSQL should remove NUL bytes, got %q", got)
	}
	if strings.Contains(got, "\x1f") {
		t.Fatalf("escapeSQL should remove control chars, got %q", got)
	}
	if !strings.Contains(got, "\\n") {
		t.Fatalf("escapeSQL should preserve newline as escaped sequence, got %q", got)
	}
	if !strings.Contains(got, "\\'") {
		t.Fatalf("escapeSQL should escape single quotes, got %q", got)
	}
	if !strings.Contains(got, "\\\\") {
		t.Fatalf("escapeSQL should escape backslashes, got %q", got)
	}
}

func TestEscapeQuery_EscapesOperators(t *testing.T) {
	in := `url:https://a.b/c?q=1&(x)-y`
	got := escapeQuery(in, false)

	mustContain := []string{"\\:", "\\/", "\\?", "\\=", "\\&", "\\(", "\\)", "\\-"}
	for _, token := range mustContain {
		if !strings.Contains(got, token) {
			t.Fatalf("escapeQuery(%q) missing %q in %q", in, token, got)
		}
	}
}

func TestEscapeQuery_KeepWildcards(t *testing.T) {
	in := `login* error*`
	got := escapeQuery(in, true)

	if strings.Contains(got, "\\*") {
		t.Fatalf("escapeQuery should keep wildcards when enabled, got %q", got)
	}
}

func TestEscapeSQL_InjectionLike(t *testing.T) {
	in := "x'; DROP TABLE issues; --"
	got := escapeSQL(in)
	if !strings.Contains(got, "\\'") {
		t.Fatalf("expected escaped quote, got %q", got)
	}
	for i := 0; i < len(got); i++ {
		if got[i] == '\'' {
			if i == 0 || got[i-1] != '\\' {
				t.Fatalf("found unescaped quote in %q", got)
			}
		}
	}
}

func TestEscapeQuery_InjectionLike(t *testing.T) {
	in := "@title:login | status:open"
	got := escapeQuery(in, false)
	if !strings.Contains(got, "\\|") {
		t.Fatalf("expected escaped pipe, got %q", got)
	}
	if !strings.Contains(got, "\\@") {
		t.Fatalf("expected escaped at, got %q", got)
	}
}

func TestBuildPrefixVariant_AddsWildcards(t *testing.T) {
	got := buildPrefixVariant("login error")
	if got != "login* error*" {
		t.Fatalf("expected prefix wildcard variant, got %q", got)
	}
}

func TestBuildInfixVariant_AddsWildcards(t *testing.T) {
	got := buildInfixVariant("login error")
	if got != "*login* *error*" {
		t.Fatalf("expected infix wildcard variant, got %q", got)
	}
}

func TestBuildQueryCondition_AddsPrefixVariant(t *testing.T) {
	cond := buildQueryCondition("login error", "url")
	if !strings.Contains(cond, "login*") {
		t.Fatalf("expected prefix wildcard in condition, got %q", cond)
	}
	if !strings.Contains(cond, "*login*") {
		t.Fatalf("expected infix wildcard in condition, got %q", cond)
	}
}

func TestBuildQueryCondition_SingleMatchClause(t *testing.T) {
	cond := buildQueryCondition("кнопка", "url")
	if strings.Count(cond, "MATCH(") != 1 {
		t.Fatalf("expected single MATCH clause, got %q", cond)
	}
	if !strings.Contains(cond, " | ") {
		t.Fatalf("expected OR operator inside MATCH expression, got %q", cond)
	}
}

func TestBuildQueryCondition_URLAddsLike(t *testing.T) {
	cond := buildQueryCondition("https://tracker.yandex.ru/PRJ-123", "url")
	if !strings.Contains(cond, "MATCH(") {
		t.Fatalf("expected MATCH in condition, got %q", cond)
	}
	if !strings.Contains(cond, "url LIKE") {
		t.Fatalf("expected LIKE fallback for URL query, got %q", cond)
	}
}

func TestBuildMatchVariants_URL(t *testing.T) {
	variants := buildMatchVariants("https://tracker.yandex.ru/PRJ-123?x=1")
	joined := strings.Join(variants, " | ")

	if !strings.Contains(joined, "tracker.yandex.ru") {
		t.Fatalf("expected host variant, got %q", joined)
	}
	if !strings.Contains(joined, "PRJ") {
		t.Fatalf("expected normalized path tokens, got %q", joined)
	}
}

func TestBuildWhereClause_URLWithFilters(t *testing.T) {
	filters := SearchFilters{
		Queue:    "DEV",
		Status:   "Open",
		Priority: "high",
	}

	where := buildWhereClause("https://tracker.yandex.ru/DEV-10", filters, "url")

	checks := []string{
		"MATCH(",
		"url LIKE",
		"queue = 'DEV'",
		"status_name = 'Open'",
		"priority = 'high'",
		" AND ",
	}

	for _, check := range checks {
		if !strings.Contains(where, check) {
			t.Fatalf("where clause %q does not contain %q", where, check)
		}
	}
}

func TestBuildWhereClause_FileURLField(t *testing.T) {
	where := buildWhereClause("https://tracker.yandex.ru/DEV-10", SearchFilters{}, "file_url")
	if !strings.Contains(where, "file_url LIKE") {
		t.Fatalf("expected file_url LIKE fallback, got %q", where)
	}
}
