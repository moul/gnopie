package main

import (
	"context"
	"testing"

	"github.com/gnolang/gno/gno.land/pkg/integration"
	"github.com/gnolang/gno/tm2/pkg/commands"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integration tests against a real in-memory gno node. Each one doubles as a
// usage example: the comment is the command line, the body is what it prints.
//
// Read-only tests take the shared node (sharedEnv). Anything that broadcasts
// takes its own (newEnv), because a committed transaction is visible to every
// other test on the same chain.

// --- GET, the default verb ---

func TestGET_Render(t *testing.T) {
	// gnopie gno.land/r/demo/counter
	// -> calls Render(""), returns the counter value
	assert.Equal(t, "0\n", sharedEnv(t).runOK(counterRealm))
}

func TestGET_RenderPath(t *testing.T) {
	// gnopie gno.land/r/demo/counter:somepath
	// -> calls Render("somepath"); counter ignores the path and still returns 0
	assert.Equal(t, "0\n", sharedEnv(t).runOK(counterRealm+":somepath"))
}

func TestGET_Network(t *testing.T) {
	// gnopie gno.land
	// -> network info: chain ID and block height
	out := sharedEnv(t).runOK("gno.land")
	assert.Contains(t, out, "Network: gno.land")
	assert.Contains(t, out, "Chain ID:")
	assert.Contains(t, out, "Block height:")
}

func TestGET_Address(t *testing.T) {
	// gnopie g1jg8mtutu9khhfwc4nxmuhcpftf0pajdhfvsqf5
	// -> inspects an account
	out, _ := sharedEnv(t).run(integration.DefaultAccount_Address)
	assert.Contains(t, out, "Address:")
	assert.Contains(t, out, integration.DefaultAccount_Address)
}

// The paste-a-gnoweb-URL promise, end to end rather than only through ParsePath.
func TestGET_GnowebURLs(t *testing.T) {
	env := sharedEnv(t)
	for _, in := range []string{
		"https://" + counterRealm,
		"http://" + counterRealm,
		"https://" + counterRealm + "#some-anchor",
		"https://" + counterRealm + "/",
	} {
		t.Run(in, func(t *testing.T) {
			assert.Equal(t, "0\n", env.runOK(in))
		})
	}
}

// --- EVAL ---

func TestEVAL_FunctionCall(t *testing.T) {
	// gnopie EVAL 'gno.land/r/demo/counter.Render("")'
	assert.Contains(t, sharedEnv(t).runOK("EVAL", counterRealm+`.Render("")`), `"0"`)
}

func TestEVAL_CrossingFunctionGetsCrossInjected(t *testing.T) {
	// Increment is a crossing function, so qeval needs `cross`. Evaluating a
	// state-changing function still fails ("invalid non-origin call"), which is
	// fine: what must NOT appear is the argument-arity error, because that is
	// what a missing cross looks like.
	_, stderr := sharedEnv(t).run("EVAL", counterRealm+".Increment()")
	assert.NotContains(t, stderr, "missing realm argument",
		"crossing function should have `cross` auto-injected")
}

// --- INSPECT and READ ---

func TestINSPECT_Realm(t *testing.T) {
	// gnopie INSPECT gno.land/r/demo/counter
	out := sharedEnv(t).runOK("INSPECT", counterRealm)
	assert.Contains(t, out, "Realm: "+counterRealm)
	assert.Contains(t, out, "func Increment")
	assert.Contains(t, out, "func Render")
	assert.Contains(t, out, "counter int")
}

func TestREAD_FunctionSource(t *testing.T) {
	// gnopie READ gno.land/r/demo/counter.Increment
	out := sharedEnv(t).runOK("READ", counterRealm+".Increment")
	assert.Contains(t, out, "func Increment")
	assert.Contains(t, out, "counter++")
}

func TestREAD_File(t *testing.T) {
	// gnopie READ gno.land/r/demo/counter/counter.gno
	out := sharedEnv(t).runOK("READ", counterRealm+"/counter.gno")
	assert.Contains(t, out, "package counter")
	assert.Contains(t, out, "func Increment")
	assert.Contains(t, out, "func Render")
}

func TestJSON_Output(t *testing.T) {
	// gnopie --json gno.land/r/demo/counter
	env := sharedEnv(t)
	io, outBuf, _ := newTestIO()
	require.NoError(t, dispatch(context.Background(),
		&baseCfg{home: env.home, jsonOut: true}, []string{counterRealm}, io))
	assert.Contains(t, outBuf.String(), `"pkg_path"`)
	assert.Contains(t, outBuf.String(), `"result"`)
}

// --- CALL and RUN: the paths that broadcast ---

func TestCALL_Increment(t *testing.T) {
	// gnopie CALL gno.land/r/demo/counter.Increment()
	// -> measures the gas, sizes the fee, signs, broadcasts
	env := newEnv(t, counterRealm)
	assert.Equal(t, "0\n", env.runOK(counterRealm))

	io, outBuf, errBuf := newTestIO()
	require.NoError(t, execCall(context.Background(), env.signingCfg(), counterRealm+".Increment()", io))
	assert.Contains(t, outBuf.String(), "TX committed")

	// The gas was measured rather than supplied: signingCfg pins neither number.
	assert.Contains(t, errBuf.String(), "measured",
		"the plan line should say the gas was measured")

	assert.Equal(t, "1\n", env.runOK(counterRealm))
}

func TestRUN_Increment(t *testing.T) {
	// gnopie RUN gno.land/r/demo/counter.Increment()
	// -> generates a main.gno, measures, signs, broadcasts
	env := newEnv(t, counterRealm)
	assert.Equal(t, "0\n", env.runOK(counterRealm))

	io, outBuf, _ := newTestIO()
	require.NoError(t, execRun(context.Background(), env.signingCfg(), counterRealm+".Increment()", io))
	assert.Contains(t, outBuf.String(), "TX committed")

	assert.Equal(t, "1\n", env.runOK(counterRealm))
}

// The headline feature, asserted rather than assumed.
//
// gnopie's whole claim is that it asks the chain instead of guessing, and until
// now every test that broadcast pinned gasWanted to 10,000,000, so the measuring
// code path was never executed by the suite that was supposed to cover it. This
// asserts the number came from the chain: not the pinned value, not a round
// number, and consistent with the buffer.
func TestCALL_MeasuresGasRatherThanGuessing(t *testing.T) {
	env := newEnv(t, counterRealm)

	cfg := env.signingCfg()
	client, _, err := cfg.signingClient("gno.land", discardIO())
	require.NoError(t, err)
	msg, err := cfg.callMsg(client, mustParse(t, counterRealm+".Increment()"))
	require.NoError(t, err)

	plan, err := cfg.planTx(client, callBuilder(msg))
	require.NoError(t, err)

	require.True(t, plan.Measured, "the plan must report that it asked the chain")
	require.Greater(t, plan.GasWanted, int64(0))
	require.NotEqual(t, int64(10_000_000), plan.GasWanted,
		"10,000,000 is the old hardcoded ceiling; a measurement should not land on it exactly")

	// The fee is derived from that gas, and clears the ante handler's floor.
	require.False(t, plan.FeeGiven)
	require.GreaterOrEqual(t, plan.GasFee, gasFeeFloor(plan.GasWanted),
		"a derived fee under the floor would be rejected by the ante handler")
	require.Equal(t, gasFeeFor(plan.GasWanted, defaultFeeMargin), plan.GasFee)
}

// An explicit --gas-wanted turns the measurement off, and the plan says so.
func TestCALL_ExplicitGasIsNotMeasured(t *testing.T) {
	env := newEnv(t, counterRealm)

	cfg := env.signingCfg()
	cfg.gasWanted = 12_345_678
	client, _, err := cfg.signingClient("gno.land", discardIO())
	require.NoError(t, err)
	msg, err := cfg.callMsg(client, mustParse(t, counterRealm+".Increment()"))
	require.NoError(t, err)

	plan, err := cfg.planTx(client, callBuilder(msg))
	require.NoError(t, err)
	require.False(t, plan.Measured)
	require.Equal(t, int64(12_345_678), plan.GasWanted)
	// The fee is still derived from it, which is the point: a hand-picked
	// ceiling still gets a fee that matches it.
	require.Equal(t, gasFeeFor(12_345_678, defaultFeeMargin), plan.GasFee)
}

// An explicit --gas-fee wins, including one that is deliberately generous.
func TestCALL_ExplicitFeeWins(t *testing.T) {
	env := newEnv(t, counterRealm)

	cfg := env.signingCfg()
	cfg.gasWanted = 1_000_000
	cfg.gasFee = "999999ugnot"
	client, _, err := cfg.signingClient("gno.land", discardIO())
	require.NoError(t, err)
	msg, err := cfg.callMsg(client, mustParse(t, counterRealm+".Increment()"))
	require.NoError(t, err)

	plan, err := cfg.planTx(client, callBuilder(msg))
	require.NoError(t, err)
	require.True(t, plan.FeeGiven)
	require.Equal(t, int64(999_999), plan.GasFee)
}

func TestCALL_BadFeeIsRejectedBeforeSigning(t *testing.T) {
	env := newEnv(t, counterRealm)
	cfg := env.signingCfg()
	cfg.gasWanted = 1_000_000
	cfg.gasFee = "1gnot" // not ugnot

	io, _, _ := newTestIO()
	err := execCall(context.Background(), cfg, counterRealm+".Increment()", io)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be in ugnot")
}

// discardIO is an IO for the helpers that need one but whose output is not what
// the test is about. signingCfg sets insecureNoPass, so nothing prompts.
func discardIO() commands.IO {
	io, _, _ := newTestIO()
	return io
}

func mustParse(t *testing.T, s string) *GnoPath {
	t.Helper()
	p, err := ParsePath(s)
	require.NoError(t, err)
	return p
}
