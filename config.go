package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml"
)

const defaultGasBuffer = 20 // percent

// Config holds persisted gnopie settings.
type Config struct {
	Key       string `toml:"key,omitempty"`        // default key name
	GasBuffer int    `toml:"gas_buffer,omitempty"` // gas estimation buffer percent (default 20)
	FeeMargin int    `toml:"fee_margin,omitempty"` // fee margin over the floor, percent (default 200)
}

// GetGasBuffer returns the gas buffer percentage, defaulting to 20.
func (c *Config) GetGasBuffer() int {
	if c.GasBuffer <= 0 {
		return defaultGasBuffer
	}
	return c.GasBuffer
}

// GetFeeMargin returns the fee margin over the ante handler's floor, as a
// percentage of that floor, defaulting to defaultFeeMargin. See fee.go for why
// the fee is a ratio of gas_wanted and not a number you pick.
func (c *Config) GetFeeMargin() int {
	if c.FeeMargin <= 0 {
		return defaultFeeMargin
	}
	return c.FeeMargin
}

func configPath(home string) string {
	return filepath.Join(home, "gnopie", "config.toml")
}

func LoadConfig(home string) (*Config, error) {
	path := configPath(home)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return &cfg, nil
}

func SaveConfig(home string, cfg *Config) error {
	path := configPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ConfigGet returns the value for a known config key.
func ConfigGet(cfg *Config, key string) (string, error) {
	switch key {
	case "key":
		return cfg.Key, nil
	case "gas-buffer":
		return fmt.Sprintf("%d", cfg.GetGasBuffer()), nil
	case "fee-margin":
		return fmt.Sprintf("%d", cfg.GetFeeMargin()), nil
	default:
		return "", fmt.Errorf("unknown config key %q (available: %s)", key, strings.Join(configKeys, ", "))
	}
}

func ConfigSet(cfg *Config, key, value string) error {
	switch key {
	case "key":
		cfg.Key = value
		return nil
	case "gas-buffer":
		v, err := parsePercent("gas-buffer", value, 0)
		if err != nil {
			return err
		}
		cfg.GasBuffer = v
		return nil
	case "fee-margin":
		// Floored at 100 because a fee under the ante handler's floor is a
		// rejected transaction, not a cheaper one (fee.go).
		v, err := parsePercent("fee-margin", value, 100)
		if err != nil {
			return err
		}
		cfg.FeeMargin = v
		return nil
	default:
		return fmt.Errorf("unknown config key %q (available: %s)", key, strings.Join(configKeys, ", "))
	}
}

func ConfigList(cfg *Config) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "key=%s\n", cfg.Key)
	fmt.Fprintf(&sb, "gas-buffer=%d\n", cfg.GetGasBuffer())
	fmt.Fprintf(&sb, "fee-margin=%d\n", cfg.GetFeeMargin())
	return sb.String()
}

// parsePercent reads a percentage, rejecting anything that is not a whole number
// at or above min.
//
// strconv.Atoi rather than fmt.Sscanf: Sscanf("20abc", "%d", &v) succeeds and
// leaves 20 behind, so a typo used to be silently accepted as the number it
// starts with.
func parsePercent(name, value string, min int) (int, error) {
	v, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || v < min {
		return 0, fmt.Errorf("%s must be a whole number of percent >= %d, got %q", name, min, value)
	}
	return v, nil
}

// configKeys is the set ConfigGet and ConfigSet both accept, named once so the
// two error messages cannot list different things.
var configKeys = []string{"key", "gas-buffer", "fee-margin"}
