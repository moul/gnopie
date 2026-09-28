package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/gnolang/gno/gno.land/pkg/gnoclient"
	"github.com/gnolang/gno/gno.land/pkg/sdk/vm"
	"github.com/gnolang/gno/tm2/pkg/commands"
	"github.com/gnolang/gno/tm2/pkg/std"
)

// execCall executes a realm function as a signed transaction.
func execCall(_ context.Context, cfg *baseCfg, expr string, io commands.IO) error {
	p, err := ParsePath(expr)
	if err != nil {
		return fmt.Errorf("parsing: %w", err)
	}

	if p.Kind != PathCall && p.Kind != PathSymbol {
		return fmt.Errorf("CALL expects gno.land/r/foo/bar.Func(...)")
	}

	// Generate gnokey mode
	if cfg.printGnokeyCmd {
		return printGnokeyCmd(cfg, p, io)
	}

	// Dry-run: show what would be called, no signing needed
	if cfg.dryRun {
		funcArgs := callArgs(p)
		if cfg.jsonOut {
			return outputJSON(io, map[string]any{
				"pkg_path": p.PkgPath, "func": p.Symbol, "args": funcArgs,
			})
		}
		io.Printfln("Would call: %s.%s(%s)", p.PkgPath, p.Symbol, strings.Join(funcArgs, ", "))
		return nil
	}

	client, _, err := cfg.signingClient(p.Domain, io)
	if err != nil {
		return err
	}

	msg, err := cfg.callMsg(client, p)
	if err != nil {
		return err
	}

	plan, err := cfg.planTx(client, callBuilder(msg))
	if err != nil {
		return err
	}
	plan.announce(cfg, io)

	res, err := client.Call(plan.BaseTxCfg(), msg)
	if err != nil {
		return fmt.Errorf("call: %w", err)
	}

	if cfg.jsonOut {
		return outputJSON(io, map[string]any{
			"height": res.Height, "hash": fmt.Sprintf("%X", res.Hash),
			"gas_used": res.DeliverTx.GasUsed, "gas_wanted": res.DeliverTx.GasWanted,
			"gas_fee": plan.GasFee,
			"data":    string(res.DeliverTx.Data),
		})
	}
	io.Printfln("TX committed - height: %d, hash: %X", res.Height, res.Hash)
	io.Printfln("  Gas: %d/%d, fee %s GNOT", res.DeliverTx.GasUsed, res.DeliverTx.GasWanted, gnotString(plan.GasFee))
	if len(res.DeliverTx.Data) > 0 {
		io.Printfln("  Data: %s", string(res.DeliverTx.Data))
	}
	return nil
}

// callArgs returns the call's arguments, which a bare symbol does not have.
func callArgs(p *GnoPath) []string {
	if p.Kind == PathCall {
		return p.Args
	}
	return nil
}

// callMsg builds the MsgCall for a parsed path, for the caller that already has a
// signing client.
func (c *baseCfg) callMsg(client *gnoclient.Client, p *GnoPath) (vm.MsgCall, error) {
	info, err := client.Signer.Info()
	if err != nil {
		return vm.MsgCall{}, fmt.Errorf("getting signer info: %w", err)
	}
	msg := vm.MsgCall{
		Caller:  info.GetAddress(),
		PkgPath: p.PkgPath,
		Func:    p.Symbol,
		Args:    callArgs(p),
	}
	if c.send != "" {
		coins, err := std.ParseCoins(c.send)
		if err != nil {
			return vm.MsgCall{}, fmt.Errorf("parsing --send: %w", err)
		}
		msg.Send = coins
	}
	return msg, nil
}

// callBuilder adapts a MsgCall to the planner's buildTx.
func callBuilder(msg vm.MsgCall) buildTx {
	return func(base gnoclient.BaseTxCfg) (*std.Tx, error) {
		return gnoclient.NewCallTx(base, msg)
	}
}

// planFromPath measures a transaction for a caller that has only the parsed
// path: it builds the same client and message the broadcast path would, so the
// number it reports is the number gnopie itself would use.
func planFromPath(cfg *baseCfg, p *GnoPath, io commands.IO) (txPlan, error) {
	client, _, err := cfg.signingClient(p.Domain, io)
	if err != nil {
		return txPlan{}, err
	}
	msg, err := cfg.callMsg(client, p)
	if err != nil {
		return txPlan{}, err
	}
	return cfg.planTx(client, callBuilder(msg))
}

func printGnokeyCmd(cfg *baseCfg, p *GnoPath, io commands.IO) error {
	remote, err := cfg.resolveRemote(p.Domain)
	if err != nil {
		return err
	}

	parts := []string{
		"gnokey", "maketx", "call",
		"-broadcast",
		fmt.Sprintf("-chainid=%s", remote.ChainID),
		fmt.Sprintf("-remote=%s", remote.RPC),
	}

	plan, planErr := planFromPath(cfg, p, io)
	parts = append(parts, gasFlags(cfg, plan, planErr, io)...)

	if cfg.send != "" {
		parts = append(parts, fmt.Sprintf("-send=%s", cfg.send))
	}
	parts = append(parts, fmt.Sprintf("-pkgpath=%s", p.PkgPath))
	parts = append(parts, fmt.Sprintf("-func=%s", p.Symbol))
	for _, arg := range callArgs(p) {
		parts = append(parts, fmt.Sprintf("-args=%s", arg))
	}
	parts = append(parts, keyToken(cfg, io))

	io.Println(strings.Join(parts, " \\\n  "))
	return nil
}

// gasFlags renders the -gas-wanted and -gas-fee pair for a command somebody is
// about to paste. The gas is measured or absent. Never invented.
//
// This used to print `-gas-wanted=10000000` unconditionally, which is the one
// thing this whole tool exists to stop: a plausible-looking ceiling the chain has
// never been asked about. Measured against mainnet on 2026-09-28 the same call
// needed 11,732,203, so the number it printed was not merely imprecise, it was
// short, and a command copied out of here failed exactly the way a hand-written
// one does.
//
// The estimate needs a signature, so it can prompt, and a reader who asked to
// PRINT a command did not ask to unlock a key. When it cannot be had, both flags
// are left out and the reason is printed beside them: -gas-fee alone would be
// worse than useless, because the fee only means anything as a ratio of a
// gas-wanted that is not there (fee.go).
func gasFlags(cfg *baseCfg, plan txPlan, planErr error, io commands.IO) []string {
	if planErr != nil {
		defer io.ErrPrintfln(
			"no -gas-wanted or -gas-fee above: the gas could not be measured (%v).\n"+
				"Add -simulate only to the command to have gnokey report it, or pass --gas-wanted to gnopie.",
			planErr)
		return nil
	}
	return []string{
		fmt.Sprintf("-gas-wanted=%d", plan.GasWanted),
		fmt.Sprintf("-gas-fee=%s", plan.FeeString()),
	}
}

// keyToken is the last token of a gnokey command: the key that signs it.
//
// A placeholder in a command somebody is about to paste is a command that fails
// on the last token. If no key is configured, say so on stderr and emit a name
// that reads as the mistake it is rather than as shell syntax.
func keyToken(cfg *baseCfg, io commands.IO) string {
	keyName, err := cfg.resolveKeyName()
	if err != nil {
		defer io.ErrPrintfln("no default key set, so the last token is a placeholder.\n" +
			"Set one with `gnopie config set key=<name>`, or see `gnokey list`.")
		return "YOUR_KEY_NAME"
	}
	return keyName
}
