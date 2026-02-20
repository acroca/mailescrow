package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	IMAP  IMAPConfig  `yaml:"imap"`
	Relay RelayConfig `yaml:"relay"`
	Web   WebConfig   `yaml:"web"`
	DB    DBConfig    `yaml:"db"`
}

type IMAPConfig struct {
	Host         string        `yaml:"host"`
	Port         int           `yaml:"port"` // default: 993
	Username     string        `yaml:"username"`
	Password     string        `yaml:"password"`
	TLS          bool          `yaml:"tls"`           // default: true
	PollInterval time.Duration `yaml:"poll_interval"` // default: 60s
}

type RelayConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	TLS      bool   `yaml:"tls"`
}

type WebConfig struct {
	Listen    string `yaml:"listen"`     // web UI, default :8080
	APIListen string `yaml:"api_listen"` // REST API, default :8081
}

type DBConfig struct {
	Path string `yaml:"path"`
}

// Load builds a Config from defaults, an optional YAML file, and environment
// variables. Environment variables take highest precedence; the config file is
// optional and silently ignored when missing.
//
// Environment variables (all prefixed MAILESCROW_):
//
//	MAILESCROW_IMAP_HOST          MAILESCROW_IMAP_PORT          MAILESCROW_IMAP_USERNAME
//	MAILESCROW_IMAP_PASSWORD      MAILESCROW_IMAP_TLS           MAILESCROW_IMAP_POLL_INTERVAL
//	MAILESCROW_RELAY_HOST         MAILESCROW_RELAY_PORT         MAILESCROW_RELAY_USERNAME
//	MAILESCROW_RELAY_PASSWORD     MAILESCROW_RELAY_TLS
//	MAILESCROW_WEB_LISTEN         MAILESCROW_API_LISTEN
//	MAILESCROW_DB_PATH
func Load(path string) (*Config, error) {
	cfg := &Config{
		IMAP:  IMAPConfig{Port: 993, TLS: true, PollInterval: 60 * time.Second},
		Relay: RelayConfig{Port: 587},
		Web:   WebConfig{Listen: ":8080", APIListen: ":8081"},
		DB:    DBConfig{Path: "mailescrow.db"},
	}

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read config: %w", err)
		}
		if err == nil {
			if err := yaml.Unmarshal(data, cfg); err != nil {
				return nil, fmt.Errorf("parse config: %w", err)
			}
		}
	}

	applyEnv(cfg)
	return cfg, nil
}

func applyEnv(cfg *Config) {
	envStr := func(key string) (string, bool) {
		v := os.Getenv(key)
		return v, v != ""
	}

	if v, ok := envStr("MAILESCROW_IMAP_HOST"); ok {
		cfg.IMAP.Host = v
	}
	if v, ok := envStr("MAILESCROW_IMAP_PORT"); ok {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.IMAP.Port = port
		}
	}
	if v, ok := envStr("MAILESCROW_IMAP_USERNAME"); ok {
		cfg.IMAP.Username = v
	}
	if v, ok := envStr("MAILESCROW_IMAP_PASSWORD"); ok {
		cfg.IMAP.Password = v
	}
	if v, ok := envStr("MAILESCROW_IMAP_TLS"); ok {
		cfg.IMAP.TLS, _ = strconv.ParseBool(v)
	}
	if v, ok := envStr("MAILESCROW_IMAP_POLL_INTERVAL"); ok {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.IMAP.PollInterval = d
		}
	}
	if v, ok := envStr("MAILESCROW_RELAY_HOST"); ok {
		cfg.Relay.Host = v
	}
	if v, ok := envStr("MAILESCROW_RELAY_PORT"); ok {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.Relay.Port = port
		}
	}
	if v, ok := envStr("MAILESCROW_RELAY_USERNAME"); ok {
		cfg.Relay.Username = v
	}
	if v, ok := envStr("MAILESCROW_RELAY_PASSWORD"); ok {
		cfg.Relay.Password = v
	}
	if v, ok := envStr("MAILESCROW_RELAY_TLS"); ok {
		cfg.Relay.TLS, _ = strconv.ParseBool(v)
	}
	if v, ok := envStr("MAILESCROW_WEB_LISTEN"); ok {
		cfg.Web.Listen = v
	}
	if v, ok := envStr("MAILESCROW_API_LISTEN"); ok {
		cfg.Web.APIListen = v
	}
	if v, ok := envStr("MAILESCROW_DB_PATH"); ok {
		cfg.DB.Path = v
	}
}
