package codebase

import "testing"

func TestCodebaseE2EConfigFromEnvDisabledByDefault(t *testing.T) {
	t.Setenv("CECE_CODEBASE_E2E_BASE_URL", "")
	t.Setenv("CECE_CODEBASE_E2E_API_KEY", "")
	t.Setenv("CECE_CODEBASE_E2E_AUTH_HELPER", "")

	cfg, ok := codebaseE2EConfigFromEnv()
	if ok {
		t.Fatalf("codebaseE2EConfigFromEnv() ok = true, want false; cfg = %+v", cfg)
	}
}

func TestCodebaseE2EConfigFromEnvAcceptsAuthHelper(t *testing.T) {
	t.Setenv("CECE_CODEBASE_E2E_BASE_URL", "https://codebase.example.test")
	t.Setenv("CECE_CODEBASE_E2E_API_KEY", "")
	t.Setenv("CECE_CODEBASE_E2E_AUTH_HELPER", "helper command")

	cfg, ok := codebaseE2EConfigFromEnv()
	if !ok {
		t.Fatal("codebaseE2EConfigFromEnv() ok = false, want true")
	}
	if cfg.BaseURL != "https://codebase.example.test" {
		t.Fatalf("BaseURL = %q, want test URL", cfg.BaseURL)
	}
	if cfg.AuthHelper != "helper command" {
		t.Fatalf("AuthHelper = %q, want helper command", cfg.AuthHelper)
	}
	if cfg.APIKey != "" {
		t.Fatalf("APIKey = %q, want empty", cfg.APIKey)
	}
}

func TestCodebaseE2EConfigFromEnvAcceptsAPIKey(t *testing.T) {
	t.Setenv("CECE_CODEBASE_E2E_BASE_URL", "https://codebase.example.test")
	t.Setenv("CECE_CODEBASE_E2E_API_KEY", "secret-token")
	t.Setenv("CECE_CODEBASE_E2E_AUTH_HELPER", "")

	cfg, ok := codebaseE2EConfigFromEnv()
	if !ok {
		t.Fatal("codebaseE2EConfigFromEnv() ok = false, want true")
	}
	if cfg.APIKey != "secret-token" {
		t.Fatalf("APIKey = %q, want secret-token", cfg.APIKey)
	}
	if cfg.AuthHelper != "" {
		t.Fatalf("AuthHelper = %q, want empty", cfg.AuthHelper)
	}
}

func TestRequireCodebaseE2EConfigSkipsWhenUnset(t *testing.T) {
	t.Setenv("CECE_CODEBASE_E2E_BASE_URL", "")
	t.Setenv("CECE_CODEBASE_E2E_API_KEY", "")
	t.Setenv("CECE_CODEBASE_E2E_AUTH_HELPER", "")

	skipped := false
	t.Run("capture-skip", func(t *testing.T) {
		defer func() { skipped = t.Skipped() }()
		requireCodebaseE2EConfig(t)
		t.Fatal("expected requireCodebaseE2EConfig to skip when env is unset")
	})

	if !skipped {
		t.Fatal("requireCodebaseE2EConfig should skip when env is unset")
	}
}
