package jev

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNewClientReadsEnvironment(t *testing.T) {
	t.Setenv(EnvAPIKey, " env-key ")
	t.Setenv(EnvBaseURL, "https://api.example.test/")
	t.Setenv(EnvModel, "jev-test")

	client, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client.Model() != "jev-test" {
		t.Errorf("Model() = %q, want %q", client.Model(), "jev-test")
	}
	if client.BaseURL() != "https://api.example.test" {
		t.Errorf("BaseURL() = %q, want trailing slash trimmed", client.BaseURL())
	}
	if client.apiKey != "env-key" {
		t.Errorf("apiKey = %q, want trimmed environment value", client.apiKey)
	}
}

func TestNewClientOptionsOverrideEnvironment(t *testing.T) {
	t.Setenv(EnvAPIKey, "env-key")
	t.Setenv(EnvBaseURL, "https://env.example.test")
	t.Setenv(EnvModel, "env-model")

	client, err := NewClient(
		WithAPIKey("option-key"),
		WithBaseURL("https://option.example.test/"),
		WithModel("option-model"),
		WithTimeout(3*time.Second),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client.apiKey != "option-key" {
		t.Errorf("apiKey = %q, want option value", client.apiKey)
	}
	if client.BaseURL() != "https://option.example.test" {
		t.Errorf("BaseURL() = %q, want option value", client.BaseURL())
	}
	if client.Model() != "option-model" {
		t.Errorf("Model() = %q, want option value", client.Model())
	}
	if client.timeout != 3*time.Second {
		t.Errorf("timeout = %v, want 3s", client.timeout)
	}
}

func TestNewClientDefaults(t *testing.T) {
	t.Setenv(EnvAPIKey, "key")
	t.Setenv(EnvBaseURL, "")
	t.Setenv(EnvModel, "")
	t.Setenv(EnvLogLevel, "")

	client, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client.BaseURL() != DefaultBaseURL {
		t.Errorf("BaseURL() = %q, want %q", client.BaseURL(), DefaultBaseURL)
	}
	if client.Model() != DefaultModel {
		t.Errorf("Model() = %q, want %q", client.Model(), DefaultModel)
	}
	if client.timeout != DefaultTimeout {
		t.Errorf("timeout = %v, want %v", client.timeout, DefaultTimeout)
	}
	if !reflect.DeepEqual(client.retry, DefaultRetryPolicy()) {
		t.Errorf("retry = %+v, want DefaultRetryPolicy", client.retry)
	}
}

func TestNewClientMissingAPIKey(t *testing.T) {
	t.Setenv(EnvAPIKey, "")
	client, err := NewClient()
	if client != nil {
		t.Fatal("expected nil client")
	}
	if !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("err = %v, want ErrNoAPIKey", err)
	}
	if !strings.Contains(err.Error(), EnvAPIKey) {
		t.Errorf("error %q should name %s", err, EnvAPIKey)
	}
}

func TestNewClientInvalidBaseURL(t *testing.T) {
	for _, baseURL := range []string{"not a url", "api.typesafe.ai", "://bad"} {
		t.Setenv(EnvAPIKey, "key")
		if _, err := NewClient(WithBaseURL(baseURL)); !errors.Is(err, ErrInvalidConfiguration) {
			t.Errorf("NewClient(WithBaseURL(%q)) err = %v, want ErrInvalidConfiguration", baseURL, err)
		}
	}
}

func TestNewClientRejectsInvalidOptions(t *testing.T) {
	t.Setenv(EnvAPIKey, "key")
	tests := map[string]Option{
		"negative timeout":     WithTimeout(-time.Second),
		"nil http client":      WithHTTPClient(nil),
		"nil logger":           WithLogger(nil),
		"empty model":          WithModel("   "),
		"negative max retries": WithRetryPolicy(RetryPolicy{MaxRetries: -1}),
		"bad jitter":           WithRetryPolicy(RetryPolicy{BackoffJitter: 1.5}),
		"bad status":           WithRetryPolicy(RetryPolicy{HTTPStatuses: []int{999}}),
	}
	for name, opt := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := NewClient(opt); err == nil {
				t.Fatalf("NewClient(%s) succeeded, want error", name)
			}
		})
	}
}

func TestNewClientEnvironmentLogLevel(t *testing.T) {
	t.Setenv(EnvAPIKey, "key")
	t.Setenv(EnvLogLevel, "debug")
	client, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if !client.logger.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("TYPESAFE_LOG_LEVEL=debug should enable debug logging")
	}

	t.Setenv(EnvLogLevel, "off")
	client, err = NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client.logger.Enabled(context.Background(), slog.LevelError) {
		t.Error("TYPESAFE_LOG_LEVEL=off should disable logging")
	}
}

func TestNewClientOptionWinsOverLogLevelEnv(t *testing.T) {
	t.Setenv(EnvAPIKey, "key")
	t.Setenv(EnvLogLevel, "off")
	client, err := NewClient(WithLogLevel(slog.LevelInfo))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if !client.logger.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("WithLogLevel should win over TYPESAFE_LOG_LEVEL")
	}
}

func TestCustomHeadersAreSentAndReservedHeadersWin(t *testing.T) {
	var custom, authorization string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		custom = r.Header.Get("X-Custom")
		authorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[]}`))
	},
		WithHeader("X-Custom", "value"),
		WithHeader("Authorization", "Bearer attacker"),
	)

	if _, err := client.ListModels(context.Background()); err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if custom != "value" {
		t.Errorf("X-Custom header = %q, want %q", custom, "value")
	}
	if authorization != "Bearer test-key" {
		t.Errorf("Authorization = %q, want the SDK key to win", authorization)
	}
}
