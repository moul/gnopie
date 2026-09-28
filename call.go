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
		var funcArgs []string
		if p.Kind == PathCall {
			funcArgs = p.Args
		}
		if cfg.jsonOut {
			return outputJSON(io, map[string]any{
				"pkg_path": p.PkgPath, "func": p.Symbol, "args": funcArgs,
			})
		}
		io.Printfln("Would call: %s.%s(%s)", p.PkgPath, p.Symbol, strings.Join(funcArgs, ", "))
		return nil
	}

	client, remote, err := cfg.signingClient(p.Domain, io)
	if err != nil {
		return err
	}
	_ = remote

	info, err := client.Signer.Info()
	if err != nil {
		return fmt.Errorf("getting signer info: %w", err)
	}

	var funcArgs []string
	if p.Kind == PathCall {
		funcArgs = p.Args
	}

	msg := vm.MsgCall{
		Caller:  info.GetAddress(),
		PkgPath: p.PkgPath,
		Func:    p.Symbol,
		Args:    funcArgs,
	}

	if cfg.send != "" {
		coins, err := std.ParseCoins(cfg.send)
		if err != nil {
			return fmt.Errorf("parsing --send: %w", err)
		}
		msg.Send = coins
	}

	gasWanted := cfg.gasWanted
	gasFee := cfg.gasFee

	if gasWanted == 0 {
		if !cfg.quiet {
			io.ErrPrintfln("Estimating gas...")
		}
		gasWanted, err = estimateCallGas(cfg, client, msg, gasFee)
		if err != nil {
			return err
		}
		if !cfg.quiet {
			io.ErrPrintfln("Estimated gas: %d", gasWanted)
		}
	}

	txCfg := gnoclient.BaseTxCfg{GasFee: gasFee, GasWanted: gasWanted}

	res, err := client.Call(txCfg, msg)
	if err != nil {
		return fmt.Errorf("call: %w", err)
	}

	if cfg.jsonOut {
		return outputJSON(io, map[string]any{
			"height": res.Height, "hash": fmt.Sprintf("%X", res.Hash),
			"gas_used": res.DeliverTx.GasUsed, "gas_wanted": res.DeliverTx.GasWanted,
			"data": string(res.DeliverTx.Data),
		})
	}
	io.Printfln("TX committed — height: %d, hash: %X", res.Height, res.Hash)
	io.Printfln("  Gas: %d/%d", res.DeliverTx.GasUsed, res.DeliverTx.GasWanted)
	if len(res.DeliverTx.Data) > 0 {
		io.Printfln("  Data: %s", string(res.DeliverTx.Data))
	}
	return nil
}

// estimateCallGas asks the chain what a call really costs, and adds the buffer.
//
// One implementation, two callers: the broadcast path and --print-gnokey-command.
// They used to differ, and the difference was the bug: the first simulated and the
// second printed a hardcoded 10,000,000, so the number you were handed to paste was
// not the number the tool would have used itself.
func estimateCallGas(cfg *baseCfg, client *gnoclient.Client, msg vm.MsgCall, gasFee string) (int64, error) {
	simCfg := gnoclient.BaseTxCfg{GasFee: gasFee, GasWanted: 100_000_000}
	tx, err := gnoclient.NewCallTx(simCfg, msg)
	if err != nil {
		return 0, fmt.Errorf("building sim tx: %w", err)
	}
	// Signed before simulating: the node requires a valid signature on this path.
	signedTx, err := client.SignTx(*tx, 0, 0)
	if err != nil {
		return 0, fmt.Errorf("signing for simulation: %w", err)
	}
	gasUsed, err := client.EstimateGas(signedTx)
	if err != nil {
		return 0, fmt.Errorf("gas estimation: %w", err)
	}
	gasWanted := gasUsed + gasUsed*cfg.gasBufferPercent()/100
	if gasWanted < 100_000 {
		gasWanted = 100_000
	}
	return gasWanted, nil
}

// measureGas is estimateCallGas for a caller that has only the parsed path: it
// builds the same client and message the broadcast path would.
func measureGas(cfg *baseCfg, p *GnoPath, io commands.IO) (int64, error) {
	client, _, err := cfg.signingClient(p.Domain, io)
	if err != nil {
		return 0, err
	}
	info, err := client.Signer.Info()
	if err != nil {
		return 0, fmt.Errorf("getting signer info: %w", err)
	}
	var funcArgs []string
	if p.Kind == PathCall {
		funcArgs = p.Args
	}
	msg := vm.MsgCall{
		Caller:  info.GetAddress(),
		PkgPath: p.PkgPath,
		Func:    p.Symbol,
		Args:    funcArgs,
	}
	if cfg.send != "" {
		coins, err := std.ParseCoins(cfg.send)
		if err != nil {
			return 0, fmt.Errorf("parsing --send: %w", err)
		}
		msg.Send = coins
	}
	return estimateCallGas(cfg, client, msg, cfg.gasFee)
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
	// The gas, measured or absent. Never invented.
	//
	// This printed `-gas-wanted=10000000` unconditionally, which is the one thing
	// this whole tool exists to stop: a plausible-looking ceiling that the chain
	// has never been asked about. Measured against mainnet on 2026-09-28 the same
	// call needed 11,732,203, so the number it printed was not merely imprecise,
	// it was short, and a command copied out of here failed exactly the way a
	// hand-written one does.
	//
	// The estimate needs a signature, so it can prompt, and a reader who asked to
	// *print* a command did not ask to unlock a key. When it cannot be had the
	// flag is left out and the reason is printed beside it. gnokey then reports
	// its own `suggested gas-wanted` on the first run, which is a round trip but
	// an honest one.
	measured, gasErr := measureGas(cfg, p, io)
	switch {
	case cfg.gasWanted > 0:
		parts = append(parts, fmt.Sprintf("-gas-wanted=%d", cfg.gasWanted))
	case gasErr == nil:
		parts = append(parts, fmt.Sprintf("-gas-wanted=%d", measured))
	default:
		defer io.ErrPrintfln("no -gas-wanted above: the gas could not be measured (%v).\n"+
			"Add -simulate only to the command to have gnokey report it, or pass -gas-wanted to gnopie.", gasErr)
	}
	parts = append(parts, fmt.Sprintf("-gas-fee=%s", cfg.gasFee))
	if cfg.send != "" {
		parts = append(parts, fmt.Sprintf("-send=%s", cfg.send))
	}
	parts = append(parts, fmt.Sprintf("-pkgpath=%s", p.PkgPath))
	parts = append(parts, fmt.Sprintf("-func=%s", p.Symbol))
	if p.Kind == PathCall {
		for _, arg := range p.Args {
			parts = append(parts, fmt.Sprintf("-args=%s", arg))
		}
	}
	// A placeholder in a command somebody is about to paste is a command that
	// fails on the last token. If no key is configured, say so as a comment and
	// name the one gnokey would prompt for anyway.
	keyName, err := cfg.resolveKeyName()
	if err != nil {
		parts = append(parts, "YOUR_KEY_NAME")
		defer io.ErrPrintfln("no default key set, so the last token is a placeholder.\n" +
			"Set one with `gnopie config set key=<name>`, or see `gnokey list`.")
	} else {
		parts = append(parts, keyName)
	}
	io.Println(strings.Join(parts, " \\\n  "))
	return nil
}
