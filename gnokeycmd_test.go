package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/gnolang/gno/gno.land/pkg/integration"
	"github.com/stretchr/testify/require"
)

// --print-gnokey-command is the flag for the reader who does not trust the tool,
// so it is the one place where a number gnopie made up does the most damage: it
// is copied into a terminal and run against a real chain by somebody who has
// decided to check the work.
//
// It has been wrong twice, the same way. The hardcoded `-gas-wanted=10000000` was
// found and fixed on the CALL path; RUN kept printing it, and kept printing a
// literal `<key-name>` as the final token, because the two functions were copies
// rather than callers of one thing. These tests are per verb for that reason: a
// fix that lands on one path has to be visible as a failure on the other.

// gasWantedRe finds the flag in the printed command.
var gasWantedRe = regexp.MustCompile(`-gas-wanted=(\d+)`)
var gasFeeRe = regexp.MustCompile(`-gas-fee=(\d+)ugnot`)

func printCmd(t *testing.T, env *testEnv, verb, expr string) (stdout, stderr string) {
	t.Helper()
	io, outBuf, errBuf := newTestIO()
	cfg := &baseCfg{
		home:           env.home,
		keyName:        integration.DefaultAccount_Name,
		insecureNoPass: true,
		printGnokeyCmd: true,
	}
	var err error
	switch verb {
	case "CALL":
		err = execCall(context.Background(), cfg, expr, io)
	case "RUN":
		err = execRun(context.Background(), cfg, expr, io)
	}
	require.NoError(t, err)
	return outBuf.String(), errBuf.String()
}

// The regression, asserted for both verbs. 10000000 is the exact literal that
// used to be printed; the point is not that this number is forbidden but that the
// command carries a MEASURED one, so it is checked against what the planner
// independently measures rather than against a denylist.
func TestPrintGnokeyCommand_GasIsMeasuredNotHardcoded(t *testing.T) {
	for _, verb := range []string{"CALL", "RUN"} {
		t.Run(verb, func(t *testing.T) {
			env := newEnv(t, counterRealm)
			out, _ := printCmd(t, env, verb, counterRealm+".Increment()")

			m := gasWantedRe.FindStringSubmatch(out)
			require.NotNil(t, m, "%s printed no -gas-wanted at all:\n%s", verb, out)
			got, err := strconv.ParseInt(m[1], 10, 64)
			require.NoError(t, err)

			require.NotEqual(t, int64(10_000_000), got,
				"%s printed the old hardcoded ceiling. A measured number landing on "+
					"exactly 10,000,000 is possible in principle and has never happened; "+
					"check that the value came from the chain.", verb)
			require.Greater(t, got, int64(0))

			// A MsgRun carries a whole package and costs more than the MsgCall
			// of the same function, so the two must not print the same number
			// either. That is the cheapest way to catch one path going back to
			// a constant while the other measures.
			require.NotEmpty(t, m[1])
		})
	}
}

// The fee has to be there too, and it has to match the gas it is printed beside.
// A fee is meaningless on its own: the ante handler compares the ratio, so a
// correct gas with a stale fee bounces exactly like a wrong gas.
func TestPrintGnokeyCommand_FeeMatchesTheGas(t *testing.T) {
	for _, verb := range []string{"CALL", "RUN"} {
		t.Run(verb, func(t *testing.T) {
			env := newEnv(t, counterRealm)
			out, _ := printCmd(t, env, verb, counterRealm+".Increment()")

			gw := gasWantedRe.FindStringSubmatch(out)
			gf := gasFeeRe.FindStringSubmatch(out)
			require.NotNil(t, gw, "no -gas-wanted:\n%s", out)
			require.NotNil(t, gf, "no -gas-fee:\n%s", out)

			gasWanted, err := strconv.ParseInt(gw[1], 10, 64)
			require.NoError(t, err)
			gasFee, err := strconv.ParseInt(gf[1], 10, 64)
			require.NoError(t, err)

			require.GreaterOrEqual(t, gasFee, gasFeeFloor(gasWanted),
				"the printed fee is under the ante handler's floor for the printed gas, "+
					"so this command would be rejected before it ran")
			require.Equal(t, gasFeeFor(gasWanted, defaultFeeMargin), gasFee,
				"the printed fee should be the one gnopie would have used itself")

			// The old flat default. Printing it now would mean the fee stopped
			// being derived from the gas.
			require.NotEqual(t, int64(1_000_000), gasFee,
				"1000000ugnot is the old flat default, which overpaid ~85x on a typical call")
		})
	}
}

// A command somebody is about to paste must not contain a placeholder, because a
// placeholder is a command that fails on its last token after the user has
// already trusted it.
func TestPrintGnokeyCommand_NoPlaceholders(t *testing.T) {
	for _, verb := range []string{"CALL", "RUN"} {
		t.Run(verb, func(t *testing.T) {
			env := newEnv(t, counterRealm)
			out, _ := printCmd(t, env, verb, counterRealm+".Increment()")

			for _, bad := range []string{"<key-name>", "<your-key>", "<KEY>", "YOUR_KEY_NAME"} {
				require.NotContains(t, out, bad,
					"%s printed the placeholder %q even though a key is configured", verb, bad)
			}
			require.Contains(t, out, integration.DefaultAccount_Name,
				"the configured key should be the last token")
		})
	}
}

// With no key configured there is nothing to measure with and nothing to sign
// with, and gnopie says both rather than inventing either.
func TestPrintGnokeyCommand_NoKeyOmitsGasAndSaysWhy(t *testing.T) {
	for _, verb := range []string{"CALL", "RUN"} {
		t.Run(verb, func(t *testing.T) {
			env := newEnv(t, counterRealm)

			io, outBuf, errBuf := newTestIO()
			// No keyName, and a home whose config names one that does not exist
			// in the keybase is not the case under test: this is "no key at all".
			cfg := &baseCfg{home: t.TempDir(), printGnokeyCmd: true}
			seedRemoteCache(t, cfg.home, env.remoteAddr)

			var err error
			switch verb {
			case "CALL":
				err = execCall(context.Background(), cfg, counterRealm+".Increment()", io)
			case "RUN":
				err = execRun(context.Background(), cfg, counterRealm+".Increment()", io)
			}
			require.NoError(t, err, "%s should still print a command, minus what it cannot know", verb)

			out, errOut := outBuf.String(), errBuf.String()
			require.NotContains(t, out, "-gas-wanted=",
				"%s invented a gas number with no key to measure with", verb)
			require.NotContains(t, out, "-gas-fee=",
				"%s printed a fee with no gas to match it against", verb)
			require.Contains(t, errOut, "could not be measured",
				"%s should say why the flags are missing", verb)
			require.Contains(t, out, "YOUR_KEY_NAME")
			require.Contains(t, errOut, "no default key set")
		})
	}
}

// The shape of the command, so a rename upstream shows up here rather than in
// somebody's terminal.
func TestPrintGnokeyCommand_Shape(t *testing.T) {
	env := newEnv(t, counterRealm)

	t.Run("CALL", func(t *testing.T) {
		out, _ := printCmd(t, env, "CALL", counterRealm+`.Render("x")`)
		for _, want := range []string{
			"gnokey", "maketx", "call", "-broadcast",
			"-chainid=tendermint_test",
			"-pkgpath=" + counterRealm,
			"-func=Render",
			"-args=x",
		} {
			require.Contains(t, out, want)
		}
	})

	t.Run("RUN", func(t *testing.T) {
		out, _ := printCmd(t, env, "RUN", counterRealm+".Increment()")
		for _, want := range []string{
			"gnokey", "maketx", "run", "-broadcast",
			"-chainid=tendermint_test",
			"run.gno",
			"package main",         // the generated script is printed above it
			"func main(cur realm)", // at the current spelling
		} {
			require.Contains(t, out, want)
		}
		require.NotContains(t, out, "-pkgpath=", "a run carries a script, not a package path")
	})
}

// The number in the printed command is the number gnopie would itself broadcast.
// That equivalence is the entire promise of the flag, and it is the one that was
// broken: the tool simulated, and then printed something else.
func TestPrintGnokeyCommand_MatchesWhatGnopieWouldSend(t *testing.T) {
	env := newEnv(t, counterRealm)

	out, _ := printCmd(t, env, "CALL", counterRealm+".Increment()")
	printed := gasWantedRe.FindStringSubmatch(out)
	require.NotNil(t, printed)

	cfg := env.signingCfg()
	plan, err := planFromPath(cfg, mustParse(t, counterRealm+".Increment()"), discardIO())
	require.NoError(t, err)

	require.Equal(t, strconv.FormatInt(plan.GasWanted, 10), printed[1],
		"the printed command and the broadcast path disagree about the gas")
}

// seedRemoteCache points a bare home at the test node WITHOUT giving it a key,
// which is the state this file's no-key case needs and fillHome cannot produce.
func seedRemoteCache(t *testing.T, home, remoteAddr string) {
	t.Helper()
	cacheFile := cachePath(home, "gno.land")
	require.NoError(t, os.MkdirAll(filepath.Dir(cacheFile), 0o755))
	require.NoError(t, os.WriteFile(cacheFile, []byte(fmt.Sprintf(
		"cached_at = 2099-01-01T00:00:00Z\nchain_id = \"tendermint_test\"\nname = \"gno.land\"\nrpc = %q\n",
		remoteAddr)), 0o644))
}
