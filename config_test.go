package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConfigDefaults(t *testing.T) {
	t.Parallel()
	cfg := &Config{}
	require.Equal(t, defaultGasBuffer, cfg.GetGasBuffer())
	require.Equal(t, defaultFeeMargin, cfg.GetFeeMargin())

	// A zero in the file means "unset", not "no buffer": toml omitempty writes
	// nothing for zero, so a zero read back is indistinguishable from absent.
	require.Equal(t, defaultGasBuffer, (&Config{GasBuffer: 0}).GetGasBuffer())
	require.Equal(t, defaultFeeMargin, (&Config{FeeMargin: 0}).GetFeeMargin())
	require.Equal(t, 50, (&Config{GasBuffer: 50}).GetGasBuffer())
	require.Equal(t, 1000, (&Config{FeeMargin: 1000}).GetFeeMargin())
}

func TestConfigSetGet(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		key     string
		value   string
		want    string
		wantErr string
	}{
		{name: "key", key: "key", value: "moul", want: "moul"},
		{name: "gas-buffer", key: "gas-buffer", value: "50", want: "50"},
		{name: "gas-buffer zero is allowed", key: "gas-buffer", value: "0", want: "20"},
		{name: "fee-margin", key: "fee-margin", value: "300", want: "300"},

		{name: "unknown key", key: "nope", value: "x", wantErr: "unknown config key"},
		{name: "the error lists every key", key: "nope", value: "x", wantErr: "key, gas-buffer, fee-margin"},
		{name: "gas-buffer rejects words", key: "gas-buffer", value: "lots", wantErr: "whole number of percent"},
		{name: "gas-buffer rejects negatives", key: "gas-buffer", value: "-1", wantErr: "whole number of percent"},
		// Sscanf used to accept this and keep the 20, silently.
		{name: "gas-buffer rejects a trailing typo", key: "gas-buffer", value: "20%", wantErr: "whole number of percent"},
		{name: "fee-margin below the floor is refused", key: "fee-margin", value: "50", wantErr: ">= 100"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &Config{}
			err := ConfigSet(cfg, tt.key, tt.value)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			got, err := ConfigGet(cfg, tt.key)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestConfigGetUnknownKey(t *testing.T) {
	t.Parallel()
	_, err := ConfigGet(&Config{}, "nope")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown config key")
}

// The round trip that matters: what `config set` writes is what the next process
// reads, and an absent file is not an error.
func TestConfigRoundTrip(t *testing.T) {
	t.Parallel()
	home := t.TempDir()

	cfg, err := LoadConfig(home)
	require.NoError(t, err, "a missing config file is a default config, not a failure")
	require.Equal(t, "", cfg.Key)

	require.NoError(t, ConfigSet(cfg, "key", "moul"))
	require.NoError(t, ConfigSet(cfg, "gas-buffer", "35"))
	require.NoError(t, ConfigSet(cfg, "fee-margin", "400"))
	require.NoError(t, SaveConfig(home, cfg))

	back, err := LoadConfig(home)
	require.NoError(t, err)
	require.Equal(t, "moul", back.Key)
	require.Equal(t, 35, back.GetGasBuffer())
	require.Equal(t, 400, back.GetFeeMargin())
}

func TestConfigPathIsUnderHome(t *testing.T) {
	t.Parallel()
	require.Equal(t, filepath.Join("/tmp/h", "gnopie", "config.toml"), configPath("/tmp/h"))
}

func TestLoadConfigRejectsGarbage(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, "gnopie"), 0o755))
	require.NoError(t, os.WriteFile(configPath(home), []byte("this is not = = toml\n"), 0o644))

	_, err := LoadConfig(home)
	require.Error(t, err, "a corrupt config should be reported, not silently ignored")
	require.Contains(t, err.Error(), "parsing config")
}

func TestConfigList(t *testing.T) {
	t.Parallel()
	got := ConfigList(&Config{Key: "moul"})
	require.Contains(t, got, "key=moul")
	require.Contains(t, got, "gas-buffer=20")
	require.Contains(t, got, "fee-margin=200")
}

// The discovery cache is keyed by a hash of the domain, so two domains cannot
// collide and no domain can escape the cache directory through its own name.
func TestCachePath(t *testing.T) {
	t.Parallel()
	a := cachePath("/tmp/h", "gno.land")
	b := cachePath("/tmp/h", "test.gno.land")
	require.NotEqual(t, a, b)
	require.Equal(t, a, cachePath("/tmp/h", "gno.land"), "the same domain must hit the same file")
	require.Equal(t, filepath.Join("/tmp/h", "gnopie", "cache"), filepath.Dir(a))

	// A domain is attacker-influenced input in the sense that matters here: it
	// comes from a URL somebody pasted. Hashing means no separator in it can
	// reach the filesystem.
	evil := cachePath("/tmp/h", "../../etc/passwd")
	require.Equal(t, filepath.Join("/tmp/h", "gnopie", "cache"), filepath.Dir(evil))
}

func TestQueryCacheKeyDistinguishesPathAndData(t *testing.T) {
	t.Parallel()
	// The separator matters: without it, ("ab", "c") and ("a", "bc") would hash
	// the same and one query would serve the other's cached answer.
	require.NotEqual(t, queryCacheKey("ab", "c"), queryCacheKey("a", "bc"))
	require.Equal(t, queryCacheKey("vm/qfile", "gno.land/r/x"), queryCacheKey("vm/qfile", "gno.land/r/x"))
	require.NotEqual(t, queryCacheKey("vm/qfile", "x"), queryCacheKey("vm/qfuncs", "x"))
}

func TestQueryCacheRoundTrip(t *testing.T) {
	t.Parallel()
	home := t.TempDir()

	_, ok := loadCachedQuery(home, "vm/qfile", "gno.land/r/x")
	require.False(t, ok, "an empty cache is a miss")

	saveCachedQuery(home, "vm/qfile", "gno.land/r/x", "contents")
	got, ok := loadCachedQuery(home, "vm/qfile", "gno.land/r/x")
	require.True(t, ok)
	require.Equal(t, "contents", got)

	_, ok = loadCachedQuery(home, "vm/qfile", "gno.land/r/other")
	require.False(t, ok, "a different query must not hit this entry")
}

// Staleness is decided by mtime, so an entry older than the window is a miss
// even though the file is still there.
func TestQueryCacheExpires(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	saveCachedQuery(home, "vm/qfile", "gno.land/r/x", "contents")

	path := filepath.Join(queryCacheDir(home), queryCacheKey("vm/qfile", "gno.land/r/x"))
	stale := time.Now().Add(-(queryCacheMaxAge + time.Minute))
	require.NoError(t, os.Chtimes(path, stale, stale))

	_, ok := loadCachedQuery(home, "vm/qfile", "gno.land/r/x")
	require.False(t, ok, "an entry past the window is a miss")
}
