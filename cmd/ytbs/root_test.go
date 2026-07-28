package main

import (
	"strings"
	"testing"
)

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
	// The shape matters: it's how a goreleaser build gets eyeballed.
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
