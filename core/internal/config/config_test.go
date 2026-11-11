package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(nil, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg != Defaults {
		t.Fatalf("got %+v, want defaults %+v", cfg, Defaults)
	}
	if cfg.DatabasePath() != "/var/lib/hazelminx/core.db" {
		t.Fatalf("database path %q", cfg.DatabasePath())
	}
}

func TestFlagOverridesEnv(t *testing.T) {
	cfg, err := Load([]string{"-listen", "127.0.0.1:1"}, env(map[string]string{
		"HAZELMINX_LISTEN_ADDR": "0.0.0.0:2",
		"HAZELMINX_DATA_DIR":    "/tmp/hzx",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != "127.0.0.1:1" || cfg.DataDir != "/tmp/hzx" {
		t.Fatalf("got %+v", cfg)
	}
}

func TestValidation(t *testing.T) {
	cases := map[string][]string{
		"listen without port":            {"-listen", "localhost"},
		"relative data dir":              {"-data-dir", "data"},
		"bad log format":                 {"-log-format", "xml"},
		"agent listen no port":           {"-agent-listen", "0.0.0.0"},
		"agent listen equals management": {"-listen", "127.0.0.1:9800", "-agent-listen", "127.0.0.1:9800"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(args, env(nil)); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestAgentListenFromEnvAndFlag(t *testing.T) {
	cfg, err := Load(nil, env(map[string]string{"HAZELMINX_AGENT_LISTEN": "10.0.0.5:7001"}))
	if err != nil || cfg.AgentListenAddr != "10.0.0.5:7001" {
		t.Fatalf("env: %+v %v", cfg, err)
	}
	cfg, err = Load([]string{"-agent-listen", "127.0.0.1:7002"}, env(map[string]string{"HAZELMINX_AGENT_LISTEN": "10.0.0.5:7001"}))
	if err != nil || cfg.AgentListenAddr != "127.0.0.1:7002" {
		t.Fatalf("flag: %+v %v", cfg, err)
	}
}

func TestUnknownFlagIsAnError(t *testing.T) {
	_, err := Load([]string{"-nope"}, env(nil))
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("expected unknown flag error, got %v", err)
	}
}
