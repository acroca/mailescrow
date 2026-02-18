package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFullConfig(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")

	content := `
smtp:
  listen: ":3025"
  username: "testuser"
  password: "testpass"
relay:
  host: "smtp.relay.com"
  port: 587
  username: "relayuser"
  password: "relaypass"
  tls: true
web:
  listen: ":8080"
db:
  path: "/tmp/test.db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.SMTP.Listen != ":3025" {
		t.Errorf("smtp.listen = %q, want %q", cfg.SMTP.Listen, ":3025")
	}
	if cfg.SMTP.Username != "testuser" {
		t.Errorf("smtp.username = %q, want %q", cfg.SMTP.Username, "testuser")
	}
	if cfg.SMTP.Password != "testpass" {
		t.Errorf("smtp.password = %q, want %q", cfg.SMTP.Password, "testpass")
	}
	if cfg.Relay.Host != "smtp.relay.com" {
		t.Errorf("relay.host = %q, want %q", cfg.Relay.Host, "smtp.relay.com")
	}
	if cfg.Relay.Port != 587 {
		t.Errorf("relay.port = %d, want %d", cfg.Relay.Port, 587)
	}
	if cfg.Relay.Username != "relayuser" {
		t.Errorf("relay.username = %q, want %q", cfg.Relay.Username, "relayuser")
	}
	if cfg.Relay.Password != "relaypass" {
		t.Errorf("relay.password = %q, want %q", cfg.Relay.Password, "relaypass")
	}
	if !cfg.Relay.TLS {
		t.Error("relay.tls = false, want true")
	}
	if cfg.Web.Listen != ":8080" {
		t.Errorf("web.listen = %q, want %q", cfg.Web.Listen, ":8080")
	}
	if cfg.DB.Path != "/tmp/test.db" {
		t.Errorf("db.path = %q, want %q", cfg.DB.Path, "/tmp/test.db")
	}
}

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")

	content := `
relay:
  host: "smtp.example.com"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.SMTP.Listen != ":2525" {
		t.Errorf("default smtp.listen = %q, want %q", cfg.SMTP.Listen, ":2525")
	}
	if cfg.Relay.Port != 587 {
		t.Errorf("default relay.port = %d, want 587", cfg.Relay.Port)
	}
	if cfg.Web.Listen != ":8080" {
		t.Errorf("default web.listen = %q, want %q", cfg.Web.Listen, ":8080")
	}
	if cfg.DB.Path != "mailescrow.db" {
		t.Errorf("default db.path = %q, want %q", cfg.DB.Path, "mailescrow.db")
	}
}

func TestLoadMissingFileIsOK(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("missing config file should not error, got: %v", err)
	}
	if cfg.SMTP.Listen != ":2525" {
		t.Errorf("default smtp.listen = %q, want :2525", cfg.SMTP.Listen)
	}
}

func TestLoadEmptyPathIsOK(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("empty path should not error, got: %v", err)
	}
	if cfg.SMTP.Listen != ":2525" {
		t.Errorf("default smtp.listen = %q, want :2525", cfg.SMTP.Listen)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(cfgFile, []byte("{{invalid yaml"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(cfgFile)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestEnvVarsOverrideDefaults(t *testing.T) {
	t.Setenv("MAILESCROW_SMTP_LISTEN", ":9025")
	t.Setenv("MAILESCROW_SMTP_USERNAME", "envuser")
	t.Setenv("MAILESCROW_SMTP_PASSWORD", "envpass")
	t.Setenv("MAILESCROW_RELAY_HOST", "relay.env.com")
	t.Setenv("MAILESCROW_RELAY_PORT", "465")
	t.Setenv("MAILESCROW_RELAY_USERNAME", "relayenv")
	t.Setenv("MAILESCROW_RELAY_PASSWORD", "relayenvpass")
	t.Setenv("MAILESCROW_RELAY_TLS", "true")
	t.Setenv("MAILESCROW_WEB_LISTEN", ":9080")
	t.Setenv("MAILESCROW_DB_PATH", "/tmp/env.db")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.SMTP.Listen != ":9025" {
		t.Errorf("smtp.listen = %q, want :9025", cfg.SMTP.Listen)
	}
	if cfg.SMTP.Username != "envuser" {
		t.Errorf("smtp.username = %q, want envuser", cfg.SMTP.Username)
	}
	if cfg.SMTP.Password != "envpass" {
		t.Errorf("smtp.password = %q, want envpass", cfg.SMTP.Password)
	}
	if cfg.Relay.Host != "relay.env.com" {
		t.Errorf("relay.host = %q, want relay.env.com", cfg.Relay.Host)
	}
	if cfg.Relay.Port != 465 {
		t.Errorf("relay.port = %d, want 465", cfg.Relay.Port)
	}
	if cfg.Relay.Username != "relayenv" {
		t.Errorf("relay.username = %q, want relayenv", cfg.Relay.Username)
	}
	if cfg.Relay.Password != "relayenvpass" {
		t.Errorf("relay.password = %q, want relayenvpass", cfg.Relay.Password)
	}
	if !cfg.Relay.TLS {
		t.Error("relay.tls = false, want true")
	}
	if cfg.Web.Listen != ":9080" {
		t.Errorf("web.listen = %q, want :9080", cfg.Web.Listen)
	}
	if cfg.DB.Path != "/tmp/env.db" {
		t.Errorf("db.path = %q, want /tmp/env.db", cfg.DB.Path)
	}
}

func TestEnvVarsOverrideConfigFile(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte("smtp:\n  listen: \":3025\"\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("MAILESCROW_SMTP_LISTEN", ":9999")

	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.SMTP.Listen != ":9999" {
		t.Errorf("smtp.listen = %q, want :9999 (env should override file)", cfg.SMTP.Listen)
	}
}
