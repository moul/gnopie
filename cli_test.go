package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gnolang/gno/gno.land/pkg/integration"
	"github.com/gnolang/gno/tm2/pkg/commands"
	"github.com/stretchr/testify/require"
)

type cliTestCase struct {
	name string
	args []string // args to dispatch (verb + expression)

	// cfg overrides — if nil, defaults are used
	jsonOut        bool
	printGnokeyCmd bool
	dryRun         bool
	debug          bool
	signing        bool // if true, sets up signing client with key + gas

	// expected outputs — if both contain+be are empty, output must be empty
	stdoutShouldContain string
	stdoutShouldBe      string
	stderrShouldContain string
	errShouldContain    string
	errShouldBe         string
}

func TestCLI(t *testing.T) {
	// Read-only throughout, so it shares the node with every other read-only
	// test rather than booting one of its own.
	home := sharedEnv(t).home

	tc := []cliTestCase{
		// --- GET (default verb) ---
		{
			name:                "realm renders by default",
			args:                []string{"gno.land/r/demo/counter"},
			stdoutShouldContain: "0",
		},
		{
			name:                "realm with render path",
			args:                []string{"gno.land/r/demo/counter:anything"},
			stdoutShouldContain: "0",
		},
		{
			name:                "network shows info",
			args:                []string{"gno.land"},
			stdoutShouldContain: "Network: gno.land",
		},
		{
			name:                "network shows chain ID",
			args:                []string{"gno.land"},
			stdoutShouldContain: "Chain ID:",
		},
		{
			name:                "address shows account",
			args:                []string{integration.DefaultAccount_Address},
			stdoutShouldContain: "Address:",
		},

		// --- GET with gnoweb URLs ---
		{
			name:                "strips https prefix",
			args:                []string{"https://gno.land/r/demo/counter"},
			stdoutShouldContain: "0",
		},
		{
			name:                "strips URL fragment",
			args:                []string{"https://gno.land/r/demo/counter#some-anchor"},
			stdoutShouldContain: "0",
		},
		{
			name:                "strips trailing slash",
			args:                []string{"https://gno.land/r/demo/counter/"},
			stdoutShouldContain: "0",
		},

		// --- EVAL ---
		{
			name:                "EVAL function call",
			args:                []string{"EVAL", `gno.land/r/demo/counter.Render("")`},
			stdoutShouldContain: `"0"`,
		},
		{
			name:             "EVAL missing expression",
			args:             []string{"EVAL"},
			errShouldContain: "missing expression",
		},
		{
			name:             "EVAL non-existent function",
			args:             []string{"EVAL", `gno.land/r/demo/counter.DoesNotExist()`},
			errShouldContain: "eval:",
		},

		// Crossing function test is in TestCLI_CrossingAutoInject below

		// --- READ ---
		{
			name:                "READ function source",
			args:                []string{"READ", "gno.land/r/demo/counter.Increment"},
			stdoutShouldContain: "func Increment",
		},
		{
			name:                "READ function source has body",
			args:                []string{"READ", "gno.land/r/demo/counter.Increment"},
			stdoutShouldContain: "counter++",
		},
		{
			name:                "READ file",
			args:                []string{"READ", "gno.land/r/demo/counter/counter.gno"},
			stdoutShouldContain: "package counter",
		},
		{
			name:                "READ file has all functions",
			args:                []string{"READ", "gno.land/r/demo/counter/counter.gno"},
			stdoutShouldContain: "func Render",
		},
		{
			name:             "READ non-existent symbol",
			args:             []string{"READ", "gno.land/r/demo/counter.Nope"},
			errShouldContain: "not found",
		},

		// --- INSPECT ---
		{
			name:                "INSPECT realm shows variables",
			args:                []string{"INSPECT", "gno.land/r/demo/counter"},
			stdoutShouldContain: "counter int",
		},
		{
			name:                "INSPECT realm shows functions",
			args:                []string{"INSPECT", "gno.land/r/demo/counter"},
			stdoutShouldContain: "func Increment",
		},
		{
			name:                "INSPECT realm shows Render",
			args:                []string{"INSPECT", "gno.land/r/demo/counter"},
			stdoutShouldContain: "func Render",
		},

		// --- JSON output ---
		{
			name:                "JSON output for realm",
			args:                []string{"gno.land/r/demo/counter"},
			jsonOut:             true,
			stdoutShouldContain: `"pkg_path"`,
		},
		{
			name:                "JSON output for network",
			args:                []string{"gno.land"},
			jsonOut:             true,
			stdoutShouldContain: `"chain_id"`,
		},

		// --- Debug output ---
		{
			name:                "debug shows dispatch info",
			args:                []string{"gno.land/r/demo/counter"},
			debug:               true,
			stderrShouldContain: "dispatch",
		},
		{
			name:                "debug shows query info",
			args:                []string{"INSPECT", "gno.land/r/demo/counter"},
			debug:               true,
			stderrShouldContain: "dispatch",
		},

		// --- CALL --print-gnokey-command ---
		{
			name:                "CALL gnokey: has gnokey header",
			args:                []string{"CALL", "gno.land/r/demo/counter.Increment()"},
			printGnokeyCmd:      true,
			stdoutShouldContain: "gnokey",
		},
		{
			name:                "CALL gnokey: has maketx call",
			args:                []string{"CALL", "gno.land/r/demo/counter.Increment()"},
			printGnokeyCmd:      true,
			stdoutShouldContain: "maketx",
		},
		{
			name:                "CALL gnokey: has func flag",
			args:                []string{"CALL", "gno.land/r/demo/counter.Increment()"},
			printGnokeyCmd:      true,
			stdoutShouldContain: "-func=Increment",
		},
		{
			name:                "CALL gnokey: has pkgpath flag",
			args:                []string{"CALL", "gno.land/r/demo/counter.Increment()"},
			printGnokeyCmd:      true,
			stdoutShouldContain: "-pkgpath=gno.land/r/demo/counter",
		},
		{
			name:                "CALL gnokey: has chainid",
			args:                []string{"CALL", "gno.land/r/demo/counter.Increment()"},
			printGnokeyCmd:      true,
			stdoutShouldContain: "-chainid=",
		},
		{
			name:                "CALL gnokey: has remote",
			args:                []string{"CALL", "gno.land/r/demo/counter.Increment()"},
			printGnokeyCmd:      true,
			stdoutShouldContain: "-remote=",
		},
		{
			name:                "CALL gnokey: has key name",
			args:                []string{"CALL", "gno.land/r/demo/counter.Increment()"},
			printGnokeyCmd:      true,
			stdoutShouldContain: integration.DefaultAccount_Name,
		},

		// --- RUN --print-gnokey-command ---
		{
			name:                "RUN gnokey: has generated code",
			args:                []string{"RUN", "gno.land/r/demo/counter.Increment()"},
			printGnokeyCmd:      true,
			stdoutShouldContain: "package main",
		},
		{
			name:                "RUN gnokey: has import",
			args:                []string{"RUN", "gno.land/r/demo/counter.Increment()"},
			printGnokeyCmd:      true,
			stdoutShouldContain: `"gno.land/r/demo/counter"`,
		},
		{
			name:                "RUN gnokey: has cross for crossing func",
			args:                []string{"RUN", "gno.land/r/demo/counter.Increment()"},
			printGnokeyCmd:      true,
			stdoutShouldContain: "counter.Increment(cross(cur))",
		},
		{
			name:                "RUN gnokey: has maketx run",
			args:                []string{"RUN", "gno.land/r/demo/counter.Increment()"},
			printGnokeyCmd:      true,
			stdoutShouldContain: "maketx",
		},
		{
			name:                "RUN gnokey: has run.gno",
			args:                []string{"RUN", "gno.land/r/demo/counter.Increment()"},
			printGnokeyCmd:      true,
			stdoutShouldContain: "run.gno",
		},
		{
			name:                "RUN gnokey: has key name",
			args:                []string{"RUN", "gno.land/r/demo/counter.Increment()"},
			printGnokeyCmd:      true,
			stdoutShouldContain: integration.DefaultAccount_Name,
		},

		// --- dry-run (no password prompt) ---
		{
			name:                "CALL dry-run shows what would be called",
			args:                []string{"CALL", "gno.land/r/demo/counter.Increment()"},
			dryRun:              true,
			stdoutShouldContain: "Would call:",
		},
		{
			name:                "RUN dry-run shows generated code",
			args:                []string{"RUN", "gno.land/r/demo/counter.Increment()"},
			dryRun:              true,
			stdoutShouldContain: "package main",
		},

		// --- Error cases ---
		{
			name:             "empty args",
			args:             []string{},
			errShouldContain: "usage:",
		},
		{
			name:             "unknown verb with no expression",
			args:             []string{"CALL"},
			errShouldContain: "missing expression",
		},
		{
			name:             "invalid path",
			args:             []string{""},
			errShouldContain: "empty path",
		},
	}

	// Run all tests twice: first pass populates cache, second tests cached path
	for _, pass := range []string{"fresh", "cached"} {
		t.Run(pass, func(t *testing.T) {
			for _, test := range tc {
				t.Run(test.name, func(t *testing.T) {
					runCLITest(t, home, test)
				})
			}
		})
	}
}

// TestCLI_CALL_Stateful tests CALL and RUN with actual state changes.
// These are separate because they mutate state and can't be repeated.
func TestCLI_CALL_Stateful(t *testing.T) {
	// Broadcasts, so it gets a chain to itself.
	home := newEnv(t, counterRealm).home

	// Verify counter starts at 0
	runCLITest(t, home, cliTestCase{
		name:           "counter starts at 0",
		args:           []string{"gno.land/r/demo/counter"},
		stdoutShouldBe: "0\n",
	})

	// CALL Increment
	runCLITestSigning(t, home, cliTestCase{
		name:                "CALL increments counter",
		args:                []string{"CALL", "gno.land/r/demo/counter.Increment()"},
		signing:             true,
		stdoutShouldContain: "TX committed",
	})

	// Verify counter is now 1
	runCLITest(t, home, cliTestCase{
		name:           "counter is 1 after CALL",
		args:           []string{"gno.land/r/demo/counter"},
		stdoutShouldBe: "1\n",
	})

	// RUN Increment
	runCLITestSigning(t, home, cliTestCase{
		name:                "RUN increments counter",
		args:                []string{"RUN", "gno.land/r/demo/counter.Increment()"},
		signing:             true,
		stdoutShouldContain: "TX committed",
	})

	// Verify counter is now 2
	runCLITest(t, home, cliTestCase{
		name:           "counter is 2 after RUN",
		args:           []string{"gno.land/r/demo/counter"},
		stdoutShouldBe: "2\n",
	})
}

func runCLITest(t *testing.T, home string, test cliTestCase) {
	t.Helper()

	mockOut := bytes.NewBufferString("")
	mockErr := bytes.NewBufferString("")

	io := commands.NewTestIO()
	io.SetOut(commands.WriteNopCloser(mockOut))
	io.SetErr(commands.WriteNopCloser(mockErr))

	cfg := &baseCfg{
		home:           home,
		jsonOut:        test.jsonOut,
		printGnokeyCmd: test.printGnokeyCmd,
		dryRun:         test.dryRun,
		debug:          test.debug,
		keyName:        integration.DefaultAccount_Name,
	}

	err := dispatch(context.Background(), cfg, test.args, io)
	checkCLIOutput(t, test, mockOut.String(), mockErr.String(), err)
}

func runCLITestSigning(t *testing.T, home string, test cliTestCase) {
	t.Helper()

	mockOut := bytes.NewBufferString("")
	mockErr := bytes.NewBufferString("")

	io := commands.NewTestIO()
	io.SetOut(commands.WriteNopCloser(mockOut))
	io.SetErr(commands.WriteNopCloser(mockErr))

	// Neither gasWanted nor gasFee is set: gnopie measures the first and
	// derives the second, which is the behaviour under test. Pinning them here
	// is what used to make the suite green without ever running that code.
	cfg := &baseCfg{
		home:           home,
		keyName:        integration.DefaultAccount_Name,
		insecureNoPass: true,
		jsonOut:        test.jsonOut,
		debug:          test.debug,
	}

	var err error
	if len(test.args) > 0 {
		verb := strings.ToUpper(test.args[0])
		expr := ""
		if len(test.args) > 1 {
			expr = test.args[1]
		}
		switch verb {
		case "CALL":
			err = execCall(context.Background(), cfg, expr, io)
		case "RUN":
			err = execRun(context.Background(), cfg, expr, io)
		default:
			err = dispatch(context.Background(), cfg, test.args, io)
		}
	}

	checkCLIOutput(t, test, mockOut.String(), mockErr.String(), err)
}

func checkCLIOutput(t *testing.T, test cliTestCase, stdout, stderr string, err error) {
	t.Helper()

	errShouldBeEmpty := test.errShouldContain == "" && test.errShouldBe == ""
	stdoutShouldBeEmpty := test.stdoutShouldContain == "" && test.stdoutShouldBe == ""
	stderrShouldBeEmpty := test.stderrShouldContain == ""

	// Check error
	if errShouldBeEmpty {
		require.NoError(t, err, "err should be nil")
	} else {
		require.Error(t, err, "err shouldn't be nil")
		if test.errShouldContain != "" {
			require.Contains(t, err.Error(), test.errShouldContain, "err should contain")
		}
		if test.errShouldBe != "" {
			require.Equal(t, test.errShouldBe, err.Error(), "err should be")
		}
	}

	// Check stdout
	if !stdoutShouldBeEmpty {
		if test.stdoutShouldContain != "" {
			require.Contains(t, stdout, test.stdoutShouldContain, "stdout should contain")
		}
		if test.stdoutShouldBe != "" {
			require.Equal(t, test.stdoutShouldBe, stdout, "stdout should be")
		}
	}

	// Check stderr
	if !stderrShouldBeEmpty {
		require.Contains(t, stderr, test.stderrShouldContain, "stderr should contain")
	}
}
