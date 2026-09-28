package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gnolang/gno/gno.land/pkg/gnoland"
	"github.com/gnolang/gno/gno.land/pkg/gnoland/ugnot"
	"github.com/gnolang/gno/gno.land/pkg/integration"
	"github.com/gnolang/gno/gnovm/pkg/gnoenv"
	"github.com/gnolang/gno/tm2/pkg/commands"
	"github.com/gnolang/gno/tm2/pkg/crypto/keys"
	"github.com/gnolang/gno/tm2/pkg/log"
	"github.com/gnolang/gno/tm2/pkg/std"
	"github.com/stretchr/testify/require"
)

// The one harness for every test that needs a chain.
//
// There were two, near-identical: cli_test.go had setupCLITestHome and
// loadCLITestPkgs, integration_test.go had setupTestEnv and loadTestPkgs, and
// they differed only in how they spelled the same TOML. Two copies of a fixture
// is one copy that goes stale, and this one had already started to: only one of
// them wrote a config.toml.
//
// It also cost real time. Every test in integration_test.go called setupTestEnv,
// and setupTestEnv boots an in-memory node, so a seventeen-test suite booted
// seventeen chains to ask read-only questions of the same genesis. Reads now
// share one node and only the tests that MUTATE state get their own.

// counterRealm is the fixture realm: it has a crossing function that changes
// state, a Render, and an exported variable, which between them cover every verb.
const counterRealm = "gno.land/r/demo/counter"

type testEnv struct {
	t          *testing.T
	home       string
	remoteAddr string
}

var (
	sharedHome string
	sharedAddr string
	sharedNode interface{ Stop() error }
)

// sharedEnv returns an environment backed by a node shared with every other
// caller. For READ-ONLY tests only: anything that broadcasts a transaction is
// visible to every other test on this node, so use newEnv instead.
func sharedEnv(t *testing.T) *testEnv {
	t.Helper()
	requireChain(t)
	return &testEnv{t: t, home: sharedHome, remoteAddr: sharedAddr}
}

// chainAvailable reports whether there is a gno source tree to boot a node from.
func chainAvailable() bool {
	_, err := os.Stat(filepath.Join(gnoenv.RootDir(), "examples"))
	return err == nil
}

// requireChain skips a test that cannot run without one, naming the reason.
func requireChain(t *testing.T) {
	t.Helper()
	if !chainAvailable() || sharedHome == "" {
		t.Skipf("needs a gno source tree: GNOROOT=%q has no examples/ (see README, Testing)",
			gnoenv.RootDir())
	}
	if testing.Short() {
		t.Skip("boots an in-memory chain; skipped under -short")
	}
}

// TestMain owns the shared node and the shared home.
//
// Both have to outlive any single test, which is exactly what t.TempDir() and
// t.Cleanup() will not do. Building them from the first test that asked looked
// right and was not: t.TempDir() is removed when THAT test returns, so the
// keybase and the pre-warmed discovery cache vanished under everything that ran
// afterwards, and the symptom was a "file is not available" from a query that had
// quietly been re-pointed at the real gno.land.
func TestMain(m *testing.M) {
	os.Exit(func() int {
		tb := &fatalTB{}

		home, err := os.MkdirTemp("", "gnopie-shared-home")
		if err != nil {
			fmt.Fprintln(os.Stderr, "creating shared home:", err)
			return 1
		}
		defer os.RemoveAll(home)

		// A missing GNOROOT SKIPS the chain tests rather than failing the run.
		//
		// Most of this package is pure: path parsing, fee arithmetic, config,
		// the generated script. None of it needs a chain, and making all of it
		// unrunnable without a matching gno checkout on disk would mean a
		// contributor cannot run anything at all until they have one. `go test
		// -short` does the same thing for the same reason.
		//
		// CI still runs the full suite, with GNOROOT checked out at the pinned
		// tag, so nothing is skipped where it counts.
		if !chainAvailable() {
			fmt.Fprintf(os.Stderr,
				"GNOROOT=%q has no examples/, so the tests that need a chain will be skipped.\n"+
					"Point it at a gnolang/gno checkout at the tag this module pins to run them\n"+
					"(see README, Testing).\n", gnoenv.RootDir())
			return m.Run()
		}

		node, addr := startNode(tb, counterRealm)
		sharedNode, sharedAddr = node, addr
		sharedHome = fillHome(tb, home, addr)
		defer func() { _ = node.Stop() }()

		return m.Run()
	}())
}

// fatalTB is a testing.TB stand-in for the setup TestMain does before any test
// exists. require needs an Errorf and a FailNow; there is no test to fail here,
// so both abort the binary with the message rather than pretending to continue.
type fatalTB struct{}

func (fatalTB) Errorf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "test setup: "+format+"\n", args...)
}
func (fatalTB) FailNow() { os.Exit(1) }
func (fatalTB) Helper()  {}

// newEnv boots a node of its own. Use it for anything that broadcasts.
func newEnv(t *testing.T, pkgs ...string) *testEnv {
	t.Helper()
	requireChain(t)
	node, addr := startNode(t, pkgs...)
	t.Cleanup(func() { _ = node.Stop() })
	return &testEnv{t: t, home: fillHome(t, t.TempDir(), addr), remoteAddr: addr}
}

// tb is the slice of testing.TB these helpers need, so the same code serves a
// real test and TestMain's setup.
type tb interface {
	Errorf(format string, args ...any)
	FailNow()
	Helper()
}

func startNode(t tb, pkgs ...string) (interface{ Stop() error }, string) {
	t.Helper()
	rootdir := gnoenv.RootDir()
	config := integration.TestingMinimalNodeConfig(rootdir)
	if len(pkgs) > 0 {
		state := config.Genesis.AppState.(gnoland.GnoGenesisState)
		state.Txs = append(state.Txs, loadPkgs(t, rootdir, pkgs...)...)
		config.Genesis.AppState = state
	}
	node, addr := integration.TestingInMemoryNode(t, log.NewNoopLogger(), config)
	return node, addr
}

// fillHome turns an empty directory into a GNOHOME that already believes gno.land
// is the test node: a pre-warmed discovery cache, a config naming the test key,
// and a keybase holding it. Without the cache entry, discovery would try to reach
// the real gno.land over the network, and the suite would depend on it.
func fillHome(t tb, home, remoteAddr string) string {
	t.Helper()
	cacheFile := cachePath(home, "gno.land")
	require.NoError(t, os.MkdirAll(filepath.Dir(cacheFile), 0o755))
	// Dated far in the future so the 24h expiry can never fire mid-suite.
	require.NoError(t, os.WriteFile(cacheFile, []byte(fmt.Sprintf(
		"cached_at = 2099-01-01T00:00:00Z\nchain_id = \"tendermint_test\"\nname = \"gno.land\"\nrpc = %q\n",
		remoteAddr)), 0o644))

	require.NoError(t, os.MkdirAll(filepath.Join(home, "gnopie"), 0o755))
	require.NoError(t, os.WriteFile(configPath(home),
		[]byte(fmt.Sprintf("key = %q\n", integration.DefaultAccount_Name)), 0o644))

	kb, err := keys.NewKeyBaseFromDir(home)
	require.NoError(t, err)
	_, err = kb.CreateAccount(
		integration.DefaultAccount_Name, integration.DefaultAccount_Seed, "", "", 0, 0)
	require.NoError(t, err)

	return home
}

func loadPkgs(t tb, rootdir string, paths ...string) []gnoland.TxWithMetadata {
	t.Helper()
	loader := integration.NewPkgsLoader()
	examplesDir := filepath.Join(rootdir, "examples")
	for _, p := range paths {
		require.NoError(t, loader.LoadPackage(examplesDir, filepath.Join(examplesDir, filepath.Clean(p)), ""),
			"loading %s out of %s. If this fails as a type error, GNOROOT is at a different "+
				"version than the gno this module pins; see the README under Testing.", p, examplesDir)
	}
	privKey, err := integration.GeneratePrivKeyFromMnemonic(integration.DefaultAccount_Seed, "", 0, 0)
	require.NoError(t, err)
	meta, err := loader.GenerateTxs(privKey,
		std.NewFee(50000, std.MustParseCoin(ugnot.ValueString(1000000))), nil)
	require.NoError(t, err)
	return meta
}

// newTestIO creates an IO that captures stdout/stderr and provides empty stdin.
func newTestIO() (commands.IO, *bytes.Buffer, *bytes.Buffer) {
	var outBuf, errBuf bytes.Buffer
	cio := &commands.IOImpl{}
	cio.SetIn(strings.NewReader("\n"))
	cio.SetOut(commands.WriteNopCloser(&outBuf))
	cio.SetErr(commands.WriteNopCloser(&errBuf))
	return cio, &outBuf, &errBuf
}

// run executes a gnopie command through the real dispatcher and returns what the
// user would have seen.
func (e *testEnv) run(args ...string) (stdout, stderr string) {
	e.t.Helper()
	io, outBuf, errBuf := newTestIO()
	err := dispatch(context.Background(), &baseCfg{home: e.home}, args, io)
	if err != nil {
		errBuf.WriteString("error: " + err.Error() + "\n")
	}
	return outBuf.String(), errBuf.String()
}

// runOK executes a gnopie command and asserts no error.
func (e *testEnv) runOK(args ...string) string {
	e.t.Helper()
	stdout, stderr := e.run(args...)
	if strings.Contains(stderr, "error:") {
		e.t.Fatalf("gnopie %v failed: %s", args, stderr)
	}
	return stdout
}

// signingCfg is the config a test uses to actually broadcast: the test key, no
// password prompt, and NOTHING pinned about gas or fees, so the measured path is
// the one under test.
//
// The stateful tests used to pass gasWanted: 10_000_000 and gasFee:
// "1000000ugnot", which meant the headline feature of the tool, measuring instead
// of guessing, was never once exercised by its own suite. Both are left at zero
// here on purpose.
func (e *testEnv) signingCfg() *baseCfg {
	return &baseCfg{
		home:           e.home,
		keyName:        integration.DefaultAccount_Name,
		insecureNoPass: true,
	}
}
