package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

type Config struct {
	SMTP  SMTPConfig  `yaml:"smtp"`
	Relay RelayConfig `yaml:"relay"`
	Web   WebConfig   `yaml:"web"`
	DB    DBConfig    `yaml:"db"`
}

type SMTPConfig struct {
	Listen   string `yaml:"listen"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type RelayConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	TLS      bool   `yaml:"tls"`
}

type WebConfig struct {
	Listen string `yaml:"listen"`
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
//	MAILESCROW_SMTP_LISTEN    MAILESCROW_SMTP_USERNAME  MAILESCROW_SMTP_PASSWORD
//	MAILESCROW_RELAY_HOST     MAILESCROW_RELAY_PORT     MAILESCROW_RELAY_USERNAME
//	MAILESCROW_RELAY_PASSWORD MAILESCROW_RELAY_TLS
//	MAILESCROW_WEB_LISTEN
//	MAILESCROW_DB_PATH
func Load(path string) (*Config, error) {
	cfg := &Config{
		SMTP:  SMTPConfig{Listen: ":2525"},
		Relay: RelayConfig{Port: 587},
		Web:   WebConfig{Listen: ":8080"},
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

	if v, ok := envStr("MAILESCROW_SMTP_LISTEN"); ok {
		cfg.SMTP.Listen = v
	}
	if v, ok := envStr("MAILESCROW_SMTP_USERNAME"); ok {
		cfg.SMTP.Username = v
	}
	if v, ok := envStr("MAILESCROW_SMTP_PASSWORD"); ok {
		cfg.SMTP.Password = v
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
	if v, ok := envStr("MAILESCROW_DB_PATH"); ok {
		cfg.DB.Path = v
	}
}
