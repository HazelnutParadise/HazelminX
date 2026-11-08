// Package config loads hazelminx-core settings from flags and environment.
//
// Precedence: command-line flag > HAZELMINX_* environment variable > default.
// Secrets never live here; this is bootstrap configuration only.
package config

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Config is the bootstrap configuration of hazelminx-core.
type Config struct {
	// ListenAddr is the management HTTP listener, e.g. "127.0.0.1:9800".
	ListenAddr string
	// DataDir holds the platform database, job records and state (37 章：/var/lib/hazelminx).
	DataDir string
	// LogFormat is "json" (default) or "text".
	LogFormat string
	// PublicOrigin is the address users open the panel at, e.g.
	// "https://panel.example.com". Passkeys are bound to its host name.
	// Empty derives it from each request.
	PublicOrigin string
}

// Defaults match the install layout in ENG.md and spec chapter 37.
var Defaults = Config{
	ListenAddr: "127.0.0.1:9800",
	DataDir:    "/var/lib/hazelminx",
	LogFormat:  "json",
}

// Load parses args (without the program name) and the environment.
func Load(args []string, getenv func(string) string) (Config, error) {
	cfg := Defaults
	if v := getenv("HAZELMINX_LISTEN_ADDR"); v != "" {
		cfg.ListenAddr = v
	}
	if v := getenv("HAZELMINX_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := getenv("HAZELMINX_LOG_FORMAT"); v != "" {
		cfg.LogFormat = v
	}
	if v := getenv("HAZELMINX_PUBLIC_ORIGIN"); v != "" {
		cfg.PublicOrigin = v
	}

	fs := flag.NewFlagSet("hazelminx-core", flag.ContinueOnError)
	fs.StringVar(&cfg.ListenAddr, "listen", cfg.ListenAddr, "management HTTP listen address")
	fs.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "platform data directory")
	fs.StringVar(&cfg.LogFormat, "log-format", cfg.LogFormat, "log format: json or text")
	fs.StringVar(&cfg.PublicOrigin, "public-origin", cfg.PublicOrigin, "address users open the panel at, e.g. https://panel.example.com (passkeys bind to its host)")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	return cfg, cfg.validate()
}

func (c Config) validate() error {
	var errs []error
	if !strings.Contains(c.ListenAddr, ":") {
		errs = append(errs, fmt.Errorf("listen address %q must be host:port", c.ListenAddr))
	}
	if !filepath.IsAbs(c.DataDir) {
		errs = append(errs, fmt.Errorf("data dir %q must be an absolute path", c.DataDir))
	}
	if c.LogFormat != "json" && c.LogFormat != "text" {
		errs = append(errs, fmt.Errorf("log format %q must be json or text", c.LogFormat))
	}
	if c.PublicOrigin != "" {
		if u, err := url.Parse(c.PublicOrigin); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
			errs = append(errs, fmt.Errorf("public origin %q must look like https://panel.example.com", c.PublicOrigin))
		}
	}
	return errors.Join(errs...)
}

// DatabasePath is the platform SQLite file inside DataDir.
func (c Config) DatabasePath() string {
	return filepath.Join(c.DataDir, "core.db")
}

// MasterKeyPath is the master key file inside DataDir. It sits beside, not
// inside, the database (spec 35.2) and marks the directory as installed.
func (c Config) MasterKeyPath() string {
	return filepath.Join(c.DataDir, "master.key")
}

// EnvGetter adapts os.Getenv for Load.
func EnvGetter() func(string) string { return os.Getenv }
