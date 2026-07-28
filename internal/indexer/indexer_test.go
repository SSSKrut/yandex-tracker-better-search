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
	in := `url:https://a.b/c&(x)-y`
	got := escapeQuery(in, false)

	// The SQL string parser eats one level of backslashes.
	mustContain := []string{`\\:`, `\\/`, `\\&`, `\\(`, `\\)`, `\\-`}
	for _, token := range mustContain {
		if !strings.Contains(got, token) {
			t.Fatalf("escapeQuery(%q) missing %q in %q", in, token, got)
		}
	}
}

func TestEscapeQuery_DoublesBackslashes(t *testing.T) {
	got := escapeQuery("a/b", false)

	if strings.Contains(got, `\\\`) {
		t.Fatalf("escapeQuery over-escaped the slash: %q", got)
	}
	if !strings.Contains(got, `\\/`) {
		t.Fatalf("escapeQuery must emit a doubled backslash before '/', got %q", got)
	}
}

func TestEscapeQuery_WildcardCharsBecomeSeparators(t *testing.T) {
	got := escapeQuery(`NOVA-42?x=1`, false)

	for _, ch := range []string{"?", "%", `\?`, `\%`} {
		if strings.Contains(got, ch) {
			t.Fatalf("escapeQuery must not keep wildcard char %q, got %q", ch, got)
		}
	}
	if !strings.Contains(got, "NOVA") || !strings.Contains(got, "42") || !strings.Contains(got, "x") {
		t.Fatalf("escapeQuery dropped query tokens: %q", got)
	}
}

func TestEscapeQuery_KeepsCyrillic(t *testing.T) {
	got := escapeQuery("кнопка отчёт", false)
	if got != "кнопка отчёт" {
		t.Fatalf("escapeQuery must not touch cyrillic text, got %q", got)
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
	cond := buildQueryCondition("login error")
	if !strings.Contains(cond, "login*") {
		t.Fatalf("expected prefix wildcard in condition, got %q", cond)
	}
	if !strings.Contains(cond, "*login*") {
		t.Fatalf("expected infix wildcard in condition, got %q", cond)
	}
}

func TestBuildQueryCondition_SingleMatchClause(t *testing.T) {
	cond := buildQueryCondition("кнопка")
	if strings.Count(cond, "MATCH(") != 1 {
		t.Fatalf("expected single MATCH clause, got %q", cond)
	}
	if !strings.Contains(cond, " | ") {
		t.Fatalf("expected OR operator inside MATCH expression, got %q", cond)
	}
}

func TestBuildQueryCondition_GroupsVariants(t *testing.T) {
	// `|` binds tighter than the implicit AND: without parentheses
	// `a b | a* b*` parses as `a (b | a*) b*` and matches nothing.
	cond := buildQueryCondition("login error")

	if !strings.Contains(cond, "(login error) | (login* error*)") {
		t.Fatalf("expected each variant wrapped in parentheses, got %q", cond)
	}
	for _, variant := range strings.Split(matchExpression(t, cond), " | ") {
		if !strings.HasPrefix(variant, "(") || !strings.HasSuffix(variant, ")") {
			t.Fatalf("variant %q is not grouped in %q", variant, cond)
		}
	}
}

func TestBuildQueryCondition_NoUnsupportedSQL(t *testing.T) {
	// Manticore supports neither LIKE on string attributes nor OR between
	// MATCH() and an attribute filter; both are a "P01: syntax error".
	cond := buildQueryCondition("https://tracker.yandex.ru/PRJ-123")

	if strings.Contains(cond, "LIKE") {
		t.Fatalf("LIKE is not supported by Manticore, got %q", cond)
	}
	if !strings.HasPrefix(cond, "MATCH(") || !strings.HasSuffix(cond, ")") {
		t.Fatalf("expected a bare MATCH condition, got %q", cond)
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

func TestBuildWhereClauses_URLWithFilters(t *testing.T) {
	filters := SearchFilters{
		Queue:    "DEV",
		Status:   "Open",
		Priority: "high",
	}

	clauses := buildWhereClauses("https://tracker.yandex.ru/DEV-10", filters, "url")
	if len(clauses) != 2 {
		t.Fatalf("expected full-text and attribute clauses, got %d: %+v", len(clauses), clauses)
	}

	if !strings.Contains(clauses[0].sql, "MATCH(") || clauses[0].exact {
		t.Fatalf("first clause must be the full-text one, got %+v", clauses[0])
	}
	if strings.Contains(clauses[1].sql, "MATCH(") || !clauses[1].exact {
		t.Fatalf("second clause must be the exact attribute one, got %+v", clauses[1])
	}
	if !strings.Contains(clauses[1].sql, "issue_key = 'DEV-10'") {
		t.Fatalf("expected issue key lookup, got %q", clauses[1].sql)
	}

	// Without filters on the second clause a link sneaks an issue past the queue.
	for _, clause := range clauses {
		for _, check := range []string{"queue = 'DEV'", "status_name = 'Open'", "priority = 'high'", " AND "} {
			if !strings.Contains(clause.sql, check) {
				t.Fatalf("where clause %q does not contain %q", clause.sql, check)
			}
		}
	}
}

func TestBuildWhereClauses_FileURLField(t *testing.T) {
	clauses := buildWhereClauses("https://tracker.yandex.ru/attachments/42/report.txt", SearchFilters{}, "file_url")
	if len(clauses) != 2 {
		t.Fatalf("expected two clauses, got %d: %+v", len(clauses), clauses)
	}
	if !strings.Contains(clauses[1].sql, "REGEX(file_url,") {
		t.Fatalf("expected file_url regex fallback, got %q", clauses[1].sql)
	}
}

func TestBuildWhereClauses_PlainTextHasNoAttributeClause(t *testing.T) {
	clauses := buildWhereClauses("кнопка отчёт", SearchFilters{}, "url")
	if len(clauses) != 1 {
		t.Fatalf("plain text query needs only the full-text clause, got %+v", clauses)
	}
}

func TestBuildWhereClauses_EmptyQueryKeepsFilters(t *testing.T) {
	clauses := buildWhereClauses("", SearchFilters{Queue: "DEV"}, "url")
	if len(clauses) != 1 {
		t.Fatalf("expected a single clause, got %+v", clauses)
	}
	if clauses[0].sql != "WHERE queue = 'DEV'" {
		t.Fatalf("expected filter-only clause, got %q", clauses[0].sql)
	}
}

func TestBuildWhereClauses_WildcardOnlyQueryMatchesNothing(t *testing.T) {
	// A filters-only clause would return the whole index.
	for _, query := range []string{"%", "???", `\`} {
		if clauses := buildWhereClauses(query, SearchFilters{Queue: "DEV"}, "url"); len(clauses) != 0 {
			t.Fatalf("query %q must not produce a filter-only clause, got %+v", query, clauses)
		}
	}
}

func TestExtractIssueKey(t *testing.T) {
	cases := map[string]string{
		"NOVA-42":                                "NOVA-42",
		"nova-42":                                "NOVA-42",
		"https://tracker.yandex.ru/NOVA-42":      "NOVA-42",
		"https://tracker.yandex.ru/NOVA-42?a=1":  "NOVA-42",
		"https://tracker.yandex.ru/NOVA-42#tail": "NOVA-42",
		"tracker.yandex.ru/NOVA-42":              "NOVA-42",
		"https://tracker.yandex.ru/pages/123":    "",
		"кнопка не работает":                     "",
		"почини NOVA-42 срочно":                  "",
		"":                                       "",
	}

	for query, want := range cases {
		if got := extractIssueKey(query); got != want {
			t.Fatalf("extractIssueKey(%q) = %q, want %q", query, got, want)
		}
	}
}

func TestBuildAttributeCondition_EscapesRegex(t *testing.T) {
	cond := buildAttributeCondition("https://example.com/a.b?c=1", "url")

	if !strings.Contains(cond, "REGEX(url,") {
		t.Fatalf("expected regex condition, got %q", cond)
	}
	// The dot must not stay a regex metacharacter.
	if !strings.Contains(cond, `\\.`) {
		t.Fatalf("expected escaped dot in regex pattern, got %q", cond)
	}
	if !strings.Contains(cond, `$'`) {
		t.Fatalf("expected the url pattern to be anchored, got %q", cond)
	}
}

func TestBuildAttributeCondition_AnchorsURL(t *testing.T) {
	// Unanchored, a link to NOVA-42 also matches NOVA-42{0..9}.
	cond := buildAttributeCondition("https://tracker.yandex.ru/NOVA-42", "url")

	pattern := "https://tracker\\\\.yandex\\\\.ru/NOVA-42$"
	if !strings.Contains(cond, pattern) {
		t.Fatalf("expected anchored pattern %q in %q", pattern, cond)
	}
}

func TestBuildAttributeCondition_DropsFragment(t *testing.T) {
	cond := buildAttributeCondition("https://tracker.yandex.ru/NOVA-42#comment", "url")
	if strings.Contains(cond, "comment") {
		t.Fatalf("fragment must be dropped from the url pattern, got %q", cond)
	}
}

func TestBuildAttributeCondition_MultiWordQueryIsNotAURL(t *testing.T) {
	if cond := buildAttributeCondition("смотри https://example.com и ещё", "url"); cond != "" {
		t.Fatalf("multi-word query must not produce a url regex, got %q", cond)
	}
}

// TestBuildWhereClauses_SyntacticallySane - guards against "P01: syntax error":
// parentheses outside literals must balance and quotes must close.
func TestBuildWhereClauses_SyntacticallySane(t *testing.T) {
	queries := []string{
		"https://",
		"https://tracker.yandex.ru/NOVA-42",
		"http://a.b/c?d=1&e=(2)",
		"NOVA-42",
		`foo)`,
		`(((`,
		`"`,
		`'`,
		`\`,
		`@summary`,
		`a | b`,
		`-foo`,
		`***`,
		`%`,
		`?`,
		"кнопка отчёт",
		"C:\\Users\\test",
		"",
	}

	for _, query := range queries {
		for _, clause := range buildWhereClauses(query, SearchFilters{Queue: "NOVA"}, "url") {
			where := clause.sql
			depth, inLiteral := 0, false
			for i := 0; i < len(where); i++ {
				switch {
				case where[i] == '\\':
					i++ // экранированный символ синтаксиса не несёт
				case where[i] == '\'':
					inLiteral = !inLiteral
				case inLiteral:
				case where[i] == '(':
					depth++
				case where[i] == ')':
					depth--
				}
				if depth < 0 {
					t.Fatalf("query %q produced unbalanced ')' in %q", query, where)
				}
			}
			if depth != 0 {
				t.Fatalf("query %q produced unbalanced parens (depth %d) in %q", query, depth, where)
			}
			if inLiteral {
				t.Fatalf("query %q produced an unterminated string literal in %q", query, where)
			}
			if strings.Contains(where, "LIKE") {
				t.Fatalf("query %q produced an unsupported LIKE in %q", query, where)
			}
			if strings.Contains(where, ") OR MATCH") || strings.Contains(where, "MATCH(') OR") {
				t.Fatalf("query %q ORed MATCH with an attribute filter in %q", query, where)
			}
		}
	}
}

func matchExpression(t *testing.T, condition string) string {
	t.Helper()

	start := strings.Index(condition, "MATCH('")
	if start < 0 {
		t.Fatalf("no MATCH in %q", condition)
	}
	expr := condition[start+len("MATCH('"):]

	end := strings.LastIndex(expr, "')")
	if end < 0 {
		t.Fatalf("unterminated MATCH in %q", condition)
	}
	return expr[:end]
}
