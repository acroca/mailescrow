package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadFullConfig(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")

	content := `
imap:
  host: "imap.example.com"
  port: 993
  username: "testuser"
  password: "testpass"
  tls: true
  poll_interval: "30s"
relay:
  host: "smtp.relay.com"
  port: 587
  username: "relayuser"
  password: "relaypass"
  tls: true
  from_name: "My Service"
web:
  listen: ":8080"
  api_listen: ":8081"
  password: "hunter2"
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

	if cfg.IMAP.Host != "imap.example.com" {
		t.Errorf("imap.host = %q, want %q", cfg.IMAP.Host, "imap.example.com")
	}
	if cfg.IMAP.Port != 993 {
		t.Errorf("imap.port = %d, want 993", cfg.IMAP.Port)
	}
	if cfg.IMAP.Username != "testuser" {
		t.Errorf("imap.username = %q, want %q", cfg.IMAP.Username, "testuser")
	}
	if cfg.IMAP.Password != "testpass" {
		t.Errorf("imap.password = %q, want %q", cfg.IMAP.Password, "testpass")
	}
	if !cfg.IMAP.TLS {
		t.Error("imap.tls = false, want true")
	}
	if cfg.IMAP.PollInterval != 30*time.Second {
		t.Errorf("imap.poll_interval = %v, want 30s", cfg.IMAP.PollInterval)
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
	if cfg.Relay.FromName != "My Service" {
		t.Errorf("relay.from_name = %q, want %q", cfg.Relay.FromName, "My Service")
	}
	if cfg.Web.Listen != ":8080" {
		t.Errorf("web.listen = %q, want %q", cfg.Web.Listen, ":8080")
	}
	if cfg.Web.APIListen != ":8081" {
		t.Errorf("web.api_listen = %q, want %q", cfg.Web.APIListen, ":8081")
	}
	if cfg.Web.Password != "hunter2" {
		t.Errorf("web.password = %q, want %q", cfg.Web.Password, "hunter2")
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

	if cfg.IMAP.Port != 993 {
		t.Errorf("default imap.port = %d, want 993", cfg.IMAP.Port)
	}
	if !cfg.IMAP.TLS {
		t.Error("default imap.tls = false, want true")
	}
	if cfg.IMAP.PollInterval != 60*time.Second {
		t.Errorf("default imap.poll_interval = %v, want 60s", cfg.IMAP.PollInterval)
	}
	if cfg.Relay.Port != 587 {
		t.Errorf("default relay.port = %d, want 587", cfg.Relay.Port)
	}
	if cfg.Web.Listen != ":8080" {
		t.Errorf("default web.listen = %q, want %q", cfg.Web.Listen, ":8080")
	}
	if cfg.Web.APIListen != ":8081" {
		t.Errorf("default web.api_listen = %q, want :8081", cfg.Web.APIListen)
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
	if cfg.IMAP.Port != 993 {
		t.Errorf("default imap.port = %d, want 993", cfg.IMAP.Port)
	}
}

func TestLoadEmptyPathIsOK(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("empty path should not error, got: %v", err)
	}
	if cfg.IMAP.Port != 993 {
		t.Errorf("default imap.port = %d, want 993", cfg.IMAP.Port)
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
	t.Setenv("MAILESCROW_IMAP_HOST", "imap.env.com")
	t.Setenv("MAILESCROW_IMAP_PORT", "143")
	t.Setenv("MAILESCROW_IMAP_USERNAME", "envuser")
	t.Setenv("MAILESCROW_IMAP_PASSWORD", "envpass")
	t.Setenv("MAILESCROW_IMAP_TLS", "false")
	t.Setenv("MAILESCROW_IMAP_POLL_INTERVAL", "120s")
	t.Setenv("MAILESCROW_RELAY_HOST", "relay.env.com")
	t.Setenv("MAILESCROW_RELAY_PORT", "465")
	t.Setenv("MAILESCROW_RELAY_USERNAME", "relayenv")
	t.Setenv("MAILESCROW_RELAY_PASSWORD", "relayenvpass")
	t.Setenv("MAILESCROW_RELAY_TLS", "true")
	t.Setenv("MAILESCROW_RELAY_FROM_NAME", "Env Service")
	t.Setenv("MAILESCROW_WEB_LISTEN", ":9080")
	t.Setenv("MAILESCROW_API_LISTEN", ":9081")
	t.Setenv("MAILESCROW_WEB_PASSWORD", "envpass123")
	t.Setenv("MAILESCROW_DB_PATH", "/tmp/env.db")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.IMAP.Host != "imap.env.com" {
		t.Errorf("imap.host = %q, want imap.env.com", cfg.IMAP.Host)
	}
	if cfg.IMAP.Port != 143 {
		t.Errorf("imap.port = %d, want 143", cfg.IMAP.Port)
	}
	if cfg.IMAP.Username != "envuser" {
		t.Errorf("imap.username = %q, want envuser", cfg.IMAP.Username)
	}
	if cfg.IMAP.Password != "envpass" {
		t.Errorf("imap.password = %q, want envpass", cfg.IMAP.Password)
	}
	if cfg.IMAP.TLS {
		t.Error("imap.tls = true, want false")
	}
	if cfg.IMAP.PollInterval != 120*time.Second {
		t.Errorf("imap.poll_interval = %v, want 120s", cfg.IMAP.PollInterval)
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
	if cfg.Relay.FromName != "Env Service" {
		t.Errorf("relay.from_name = %q, want Env Service", cfg.Relay.FromName)
	}
	if cfg.Web.Listen != ":9080" {
		t.Errorf("web.listen = %q, want :9080", cfg.Web.Listen)
	}
	if cfg.Web.APIListen != ":9081" {
		t.Errorf("web.api_listen = %q, want :9081", cfg.Web.APIListen)
	}
	if cfg.Web.Password != "envpass123" {
		t.Errorf("web.password = %q, want envpass123", cfg.Web.Password)
	}
	if cfg.DB.Path != "/tmp/env.db" {
		t.Errorf("db.path = %q, want /tmp/env.db", cfg.DB.Path)
	}
}

func TestEnvVarsOverrideConfigFile(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte("imap:\n  host: \"imap.file.com\"\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("MAILESCROW_IMAP_HOST", "imap.env.com")

	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.IMAP.Host != "imap.env.com" {
		t.Errorf("imap.host = %q, want imap.env.com (env should override file)", cfg.IMAP.Host)
	}
}
