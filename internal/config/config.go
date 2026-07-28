// Package config holds every setting in one place.
//
// The environment is read once, at startup in cmd/ytbs; the values then travel
// as constructor arguments. Nothing under internal/ touches os.Getenv: hidden
// env reads don't show up in a signature, rule out two differently configured
// instances, and force tests to mutate process-wide state.
package config

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

// Config - every setting. Defaults live in the tags so they can't drift from
// the help text, which is generated from those same tags.
type Config struct {
	// Yandex Tracker
	OAuthToken string `env:"TRACKER_OAUTH_TOKEN" env-description:"OAuth token for Yandex Tracker (required for sync/serve, or use TRACKER_IAM_TOKEN)"`
	IAMToken   string `env:"TRACKER_IAM_TOKEN" env-description:"IAM (Bearer) token for Yandex Tracker; takes precedence over OAuth"`
	OrgID      string `env:"TRACKER_CLOUD_ORG_ID" env-description:"Cloud Organization ID (required for sync/serve)"`

	AttachmentTextMaxBytes int64 `env:"ATTACHMENT_TEXT_MAX_BYTES" env-default:"2097152" env-description:"Max size in bytes for downloading/indexing text attachments"`

	// Manticore
	ManticoreURL string `env:"MANTICORE_URL" env-default:"http://localhost:9308" env-description:"Manticore Search URL"`

	// Sync
	SyncStatePath string        `env:"SYNC_STATE_PATH" env-default:"backups/sync_state.json" env-description:"Path to the persisted sync state"`
	SyncOverlap   time.Duration `env:"SYNC_OVERLAP" env-default:"2m" env-description:"How far back an incremental sync reaches to absorb clock skew (e.g. 2m, 30s)"`

	// Similarity map
	MapCacheTTL       time.Duration `env:"MAP_CACHE_TTL" env-default:"10m" env-description:"How long a built similarity map stays cached (e.g. 10m, 1h)"`
	MapMaxIssues      int           `env:"MAP_MAX_ISSUES" env-default:"1000" env-description:"Max issues pulled into the similarity map"`
	MapMaxFiles       int           `env:"MAP_MAX_FILES" env-default:"1000" env-description:"Max files pulled into the similarity map"`
	MapMaxFileNames   int           `env:"MAP_MAX_FILE_NAMES" env-default:"5" env-description:"Max attachment names folded into an issue document"`
	MapMaxDocChars    int           `env:"MAP_MAX_DOC_CHARS" env-default:"4000" env-description:"Max characters taken from a single document"`
	MapMaxVocab       int           `env:"MAP_MAX_VOCAB" env-default:"1000" env-description:"Max vocabulary size for TF-IDF"`
	MapMaxNeighbors   int           `env:"MAP_MAX_NEIGHBORS" env-default:"5" env-description:"How many nearest neighbours to keep per point"`
	MapSimilarityDims int           `env:"MAP_SIM_DIMS" env-default:"16" env-description:"SVD dimensions used for similarity"`
	MapClusterK       int           `env:"MAP_CLUSTER_K" env-default:"0" env-description:"Fixed number of k-means clusters (0 = pick automatically)"`

	// MCP
	MCPAuthToken string `env:"MCP_AUTH_TOKEN" env-description:"Bearer token required by the /mcp endpoint (empty = unauthenticated)"`
}

// Load reads the environment. Unlike the helpers it replaced, a value it can't
// parse fails startup instead of quietly becoming the default - MAP_CACHE_MINUTES=10m
// used to mean 10 minutes, with nothing to tell the operator otherwise.
func Load() (*Config, error) {
	unsetEmptyVars()

	var cfg Config
	if err := cleanenv.ReadEnv(&cfg); err != nil {
		return nil, fmt.Errorf("read environment: %w", err)
	}
	return &cfg, nil
}

// unsetEmptyVars - cleanenv treats an empty value as a value and fails to parse
// `SYNC_OVERLAP=`. Empty is normal in docker-compose (`${VAR:-}`) and the old
// code read it as unset, so clear those first. The names come from the tags.
func unsetEmptyVars() {
	t := reflect.TypeOf(Config{})

	for i := 0; i < t.NumField(); i++ {
		name := t.Field(i).Tag.Get("env")
		if name == "" {
			continue
		}
		if val, ok := os.LookupEnv(name); ok && strings.TrimSpace(val) == "" {
			_ = os.Unsetenv(name)
		}
	}
}

// TrackerAuth returns the token and its scheme. IAM wins over OAuth: it's the
// newer method Yandex Cloud recommends.
func (c *Config) TrackerAuth() (token string, isIAM bool) {
	if c.IAMToken != "" {
		return c.IAMToken, true
	}
	return c.OAuthToken, false
}

// RequireTracker checks the credentials needed to call the Tracker API.
// Index-only commands (search, mcp) skip it.
func (c *Config) RequireTracker() error {
	if c.OAuthToken == "" && c.IAMToken == "" {
		return fmt.Errorf("either TRACKER_OAUTH_TOKEN or TRACKER_IAM_TOKEN environment variable is required")
	}
	if c.OrgID == "" {
		return fmt.Errorf("TRACKER_CLOUD_ORG_ID environment variable is required")
	}
	return nil
}

// Description builds the variable list for --help from the struct tags, so the
// help can't drift from what is actually read.
func Description() (string, error) {
	text, err := cleanenv.GetDescription(&Config{}, nil)
	if err != nil {
		return "", fmt.Errorf("build config description: %w", err)
	}
	return text, nil
}
