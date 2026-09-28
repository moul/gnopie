package main

import (
	"fmt"

	"github.com/gnolang/gno/gno.land/pkg/gnoclient"
	"github.com/gnolang/gno/tm2/pkg/commands"
	"github.com/gnolang/gno/tm2/pkg/std"
)

// simGasWanted is the ceiling a SIZING transaction carries. It is never charged:
// simulation reports what the messages actually used. It has to be generous,
// because a probe ceiling below the answer comes back as an out-of-gas error
// rather than as the smaller number you were asking for.
const simGasWanted = 100_000_000

// minGasWanted is the floor gnopie will ask for. Below this the buffer is noise
// and a trivially cheap transaction is one block-parameter change away from
// bouncing for a rounding error.
const minGasWanted = 100_000

// txPlan is what a transaction is about to cost: the ceiling to ask for, the fee
// that ceiling implies, and where each number came from.
//
// It exists so that CALL and RUN cannot answer the question differently. They
// used to: the fix for the hardcoded gas in --print-gnokey-command landed on the
// CALL path only, and RUN went on printing `-gas-wanted=10000000` and a
// `<key-name>` placeholder for another commit. One planner, two callers, and
// that class of drift stops being possible.
type txPlan struct {
	GasWanted int64
	GasFee    int64 // ugnot
	Measured  bool  // the chain was asked, rather than --gas-wanted supplied
	FeeGiven  bool  // --gas-fee was supplied, so the fee is not derived
}

// FeeString renders the fee as the coin string gnokey and BaseTxCfg expect.
func (p txPlan) FeeString() string { return ugnotString(p.GasFee) }

// BaseTxCfg is the plan as gnoclient wants it.
func (p txPlan) BaseTxCfg() gnoclient.BaseTxCfg {
	return gnoclient.BaseTxCfg{GasWanted: p.GasWanted, GasFee: p.FeeString()}
}

// Describe is the one line printed before a broadcast, so the number that is
// about to be spent is on screen before it is spent.
func (p txPlan) Describe() string {
	how := "from --gas-wanted"
	if p.Measured {
		how = "measured"
	}
	fee := "derived"
	if p.FeeGiven {
		fee = "from --gas-fee"
	}
	return fmt.Sprintf("gas-wanted %d (%s), gas-fee %s = %s GNOT (%s)",
		p.GasWanted, how, ugnotString(p.GasFee), gnotString(p.GasFee), fee)
}

// buildTx builds an unsigned transaction at a given BaseTxCfg. Each verb supplies
// one, closing over its own message, which is the only thing that differs between
// simulating a MsgCall and simulating a MsgRun.
type buildTx func(gnoclient.BaseTxCfg) (*std.Tx, error)

// planTx settles gas_wanted and gas_fee for a transaction, asking the chain
// unless it was told not to.
//
// Both numbers, together, on purpose. The fee is a ratio of gas_wanted, not a
// flat amount (fee.go), so measuring the gas and then keeping a fixed fee is half
// a job: it fixes the bounce the README is about and leaves the one underneath
// it. Sizing them in the same place is what makes the pair consistent.
func (c *baseCfg) planTx(client *gnoclient.Client, build buildTx) (txPlan, error) {
	plan := txPlan{GasWanted: c.gasWanted}

	if plan.GasWanted == 0 {
		gas, err := c.simulateGas(client, build)
		if err != nil {
			return txPlan{}, err
		}
		plan.GasWanted = gas
		plan.Measured = true
	}

	// An explicit --gas-fee wins, including one that is below the floor: gnopie
	// says what the floor is rather than silently overriding a number somebody
	// typed on purpose.
	if c.gasFee != "" {
		fee, err := parseUgnot(c.gasFee)
		if err != nil {
			return txPlan{}, err
		}
		plan.GasFee = fee
		plan.FeeGiven = true
		return plan, nil
	}

	plan.GasFee = gasFeeFor(plan.GasWanted, c.feeMarginPercent())
	return plan, nil
}

// simulateGas asks the chain what the transaction costs and adds the buffer.
//
// The signature is not optional. EstimateGas runs the real ante handler, which
// verifies it, so a simulation needs a key even though nothing is broadcast. That
// is the whole reason --print-gnokey-command can come back without a number: a
// reader who asked to PRINT a command did not ask to unlock a key.
func (c *baseCfg) simulateGas(client *gnoclient.Client, build buildTx) (int64, error) {
	// The probe's own fee has to clear the floor for its own ceiling, or the
	// ante handler rejects the simulation before it ever reports a number.
	simCfg := gnoclient.BaseTxCfg{
		GasWanted: simGasWanted,
		GasFee:    ugnotString(gasFeeFor(simGasWanted, defaultFeeMargin)),
	}
	tx, err := build(simCfg)
	if err != nil {
		return 0, fmt.Errorf("building simulation tx: %w", err)
	}
	signedTx, err := client.SignTx(*tx, 0, 0)
	if err != nil {
		return 0, fmt.Errorf("signing for simulation: %w", err)
	}
	gasUsed, err := client.EstimateGas(signedTx)
	if err != nil {
		return 0, fmt.Errorf("gas estimation: %w", err)
	}
	return withBuffer(gasUsed, c.gasBufferPercent()), nil
}

// withBuffer turns measured gas into a ceiling to ask for.
//
// gas_wanted is a ceiling, not a price: you are refunded the difference, so the
// buffer costs nothing and only protects against the measurement being slightly
// optimistic. The fee is the opposite (fee.go), which is why the two numbers get
// different treatment.
func withBuffer(gasUsed, bufferPct int64) int64 {
	if bufferPct < 0 {
		bufferPct = 0
	}
	gasWanted := gasUsed + gasUsed*bufferPct/100
	if gasWanted < minGasWanted {
		gasWanted = minGasWanted
	}
	return gasWanted
}

// announce prints the plan before the broadcast, unless --quiet.
func (p txPlan) announce(cfg *baseCfg, io commands.IO) {
	if !cfg.quiet {
		io.ErrPrintfln("%s", p.Describe())
	}
}
