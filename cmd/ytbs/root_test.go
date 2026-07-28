package main

import (
	"strings"
	"testing"

	"github.com/SSSKrut/yandex-tracker-better-search/internal/tracker"
)

func TestResolveTrackerAuth(t *testing.T) {
	tests := []struct {
		name       string
		iam        string
		oauth      string
		wantToken  string
		wantScheme tracker.AuthScheme
	}{
		{"только oauth", "", "oauth-token", "oauth-token", tracker.AuthOAuth},
		{"только iam", "iam-token", "", "iam-token", tracker.AuthIAM},
		{"iam выигрывает у oauth", "iam-token", "oauth-token", "iam-token", tracker.AuthIAM},
		{"ничего не задано", "", "", "", tracker.AuthOAuth},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TRACKER_IAM_TOKEN", tc.iam)
			t.Setenv("TRACKER_OAUTH_TOKEN", tc.oauth)

			token, scheme := resolveTrackerAuth()
			if token != tc.wantToken {
				t.Errorf("token = %q, want %q", token, tc.wantToken)
			}
			if scheme != tc.wantScheme {
				t.Errorf("scheme = %v, want %v", scheme, tc.wantScheme)
			}
		})
	}
}

func TestResolveManticoreURL(t *testing.T) {
	t.Run("по умолчанию", func(t *testing.T) {
		t.Setenv("MANTICORE_URL", "")
		if got := resolveManticoreURL(); got != defaultManticoreURL {
			t.Errorf("resolveManticoreURL() = %q, want %q", got, defaultManticoreURL)
		}
	})

	t.Run("из окружения", func(t *testing.T) {
		t.Setenv("MANTICORE_URL", "http://manticore:9308")
		if got := resolveManticoreURL(); got != "http://manticore:9308" {
			t.Errorf("resolveManticoreURL() = %q, want the env value", got)
		}
	})
}

func TestRequireTrackerEnv(t *testing.T) {
	// Функция читает пакетные переменные, которые обычно заполняет
	// PersistentPreRunE — в тесте выставляем их напрямую и возвращаем обратно.
	origToken, origOrg := trackerToken, trackerOrgID
	t.Cleanup(func() { trackerToken, trackerOrgID = origToken, origOrg })

	tests := []struct {
		name     string
		token    string
		orgID    string
		wantErr  bool
		errMatch string
	}{
		{"всё задано", "token", "org", false, ""},
		{"нет токена", "", "org", true, "TRACKER_OAUTH_TOKEN"},
		{"нет организации", "token", "", true, "TRACKER_CLOUD_ORG_ID"},
		{"нет ничего", "", "", true, "TRACKER_OAUTH_TOKEN"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			trackerToken, trackerOrgID = tc.token, tc.orgID

			err := RequireTrackerEnv()
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				if !strings.Contains(err.Error(), tc.errMatch) {
					t.Errorf("error %q does not mention %q", err, tc.errMatch)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestGetters(t *testing.T) {
	origToken, origOrg, origScheme := trackerToken, trackerOrgID, trackerAuthScheme
	t.Cleanup(func() {
		trackerToken, trackerOrgID, trackerAuthScheme = origToken, origOrg, origScheme
	})

	trackerToken, trackerOrgID, trackerAuthScheme = "tok", "org", tracker.AuthIAM

	if GetTrackerToken() != "tok" {
		t.Errorf("GetTrackerToken() = %q", GetTrackerToken())
	}
	if GetTrackerOrgID() != "org" {
		t.Errorf("GetTrackerOrgID() = %q", GetTrackerOrgID())
	}
	if GetTrackerAuthScheme() != tracker.AuthIAM {
		t.Errorf("GetTrackerAuthScheme() = %v", GetTrackerAuthScheme())
	}
}

func TestRootCmd_SubcommandsRegistered(t *testing.T) {
	want := map[string]bool{"serve": false, "sync": false, "search": false, "mcp": false}

	for _, cmd := range rootCmd.Commands() {
		if _, ok := want[cmd.Name()]; ok {
			want[cmd.Name()] = true
		}
	}

	for name, registered := range want {
		if !registered {
			t.Errorf("subcommand %q is not registered on rootCmd", name)
		}
	}
}

func TestRootCmd_VersionIsStamped(t *testing.T) {
	// Формат важен: по нему goreleaser-сборку проверяют глазами.
	if !strings.Contains(rootCmd.Version, version) {
		t.Errorf("rootCmd.Version = %q, want it to contain %q", rootCmd.Version, version)
	}
	for _, part := range []string{"commit", "built"} {
		if !strings.Contains(rootCmd.Version, part) {
			t.Errorf("rootCmd.Version = %q, want it to mention %q", rootCmd.Version, part)
		}
	}
}

func TestServeCmd_FlagDefaults(t *testing.T) {
	tests := map[string]string{
		"addr":          ":8080",
		"interval":      "15m0s",
		"full-interval": "24h0m0s",
	}

	for name, want := range tests {
		flag := serveCmd.Flags().Lookup(name)
		if flag == nil {
			t.Errorf("serve has no --%s flag", name)
			continue
		}
		if flag.DefValue != want {
			t.Errorf("--%s default = %q, want %q", name, flag.DefValue, want)
		}
	}
}

func TestSearchCmd_RequiresQuery(t *testing.T) {
	if err := searchCmd.Args(searchCmd, nil); err == nil {
		t.Error("search must reject an empty query")
	}
	if err := searchCmd.Args(searchCmd, []string{"кнопка"}); err != nil {
		t.Errorf("search must accept a query: %v", err)
	}
}

func TestIsLoopback(t *testing.T) {
	tests := map[string]bool{
		":8080":           true,
		"127.0.0.1:8080":  true,
		"localhost:8080":  true,
		"localhost":       true,
		"::1:8080":        true,
		"":                true,
		"0.0.0.0:8080":    false,
		"192.168.1.5:808": false,
		"example.com:80":  false,
	}

	for addr, want := range tests {
		if got := isLoopback(addr); got != want {
			t.Errorf("isLoopback(%q) = %v, want %v", addr, got, want)
		}
	}
}
