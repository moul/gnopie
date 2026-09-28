package main

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/gnolang/gno/gno.land/pkg/gnoclient"
	"github.com/gnolang/gno/gno.land/pkg/sdk/vm"
	"github.com/gnolang/gno/tm2/pkg/commands"
	"github.com/gnolang/gno/tm2/pkg/std"
)

// execRun generates Gno code that calls the expression and executes via maketx run.
// This allows importing the realm and calling with full Go syntax.
func execRun(_ context.Context, cfg *baseCfg, expr string, io commands.IO) error {
	p, err := ParsePath(expr)
	if err != nil {
		return fmt.Errorf("parsing: %w", err)
	}

	if p.Kind != PathCall {
		return fmt.Errorf("RUN expects a function call like gno.land/r/foo/bar.Func(...)")
	}

	// Check if crossing function and inject `cross` in generated code
	pkgAlias := path.Base(p.PkgPath)
	funcArgs := p.Args
	qc, _, _ := cfg.queryClient(p.Domain)
	if qc != nil && isCrossingFunc(qc, cfg, p.PkgPath, p.Symbol) {
		// Prepend "cross" as a raw token (not a string arg)
		funcArgs = append([]string{"__cross__"}, funcArgs...)
	}
	code := generateRunCode(p.PkgPath, pkgAlias, p.Symbol, funcArgs)

	if cfg.printGnokeyCmd {
		return printRunGnokeyCmd(cfg, p, code, io)
	}

	// Dry-run: just show generated code, no signing needed
	if cfg.dryRun {
		if cfg.jsonOut {
			return outputJSON(io, map[string]any{"code": code})
		}
		io.Println("Generated code:")
		io.Println(code)
		return nil
	}

	client, _, err := cfg.signingClient(p.Domain, io)
	if err != nil {
		return err
	}

	msg, err := cfg.runMsg(client, code)
	if err != nil {
		return err
	}

	plan, err := cfg.planTx(client, runBuilder(msg))
	if err != nil {
		return err
	}
	plan.announce(cfg, io)

	res, err := client.Run(plan.BaseTxCfg(), msg)
	if err != nil {
		return fmt.Errorf("run: %w", err)
	}

	if cfg.jsonOut {
		return outputJSON(io, map[string]any{
			"height": res.Height, "hash": fmt.Sprintf("%X", res.Hash),
			"gas_used": res.DeliverTx.GasUsed, "gas_wanted": res.DeliverTx.GasWanted,
			"gas_fee": plan.GasFee, "code": code,
			"data": string(res.DeliverTx.Data),
		})
	}
	io.Printfln("TX committed - height: %d, hash: %X", res.Height, res.Hash)
	io.Printfln("  Gas: %d/%d, fee %s GNOT", res.DeliverTx.GasUsed, res.DeliverTx.GasWanted, gnotString(plan.GasFee))
	return nil
}

// runMsg builds the MsgRun carrying the generated script.
func (c *baseCfg) runMsg(client *gnoclient.Client, code string) (vm.MsgRun, error) {
	info, err := client.Signer.Info()
	if err != nil {
		return vm.MsgRun{}, fmt.Errorf("getting signer info: %w", err)
	}
	msg := vm.MsgRun{
		Caller: info.GetAddress(),
		Package: &std.MemPackage{
			Name: "main",
			Path: "", // ephemeral
			Files: []*std.MemFile{
				{Name: "run.gno", Body: code},
			},
		},
	}
	if c.send != "" {
		coins, err := std.ParseCoins(c.send)
		if err != nil {
			return vm.MsgRun{}, fmt.Errorf("parsing --send: %w", err)
		}
		msg.Send = coins
	}
	return msg, nil
}

// runBuilder adapts a MsgRun to the planner's buildTx.
func runBuilder(msg vm.MsgRun) buildTx {
	return func(base gnoclient.BaseTxCfg) (*std.Tx, error) {
		return gnoclient.NewRunTx(base, msg)
	}
}

// generateRunCode emits the main.gno a MsgRun carries.
//
// The entry point takes a realm and the cross-call is explicit, because that is
// what the VM accepts: `func main()` with a bare `cross` was the older spelling
// and gno moved on. preprocess.go is unambiguous about the current one, on both
// halves:
//
//	"cross argument must be a bare realm-typed identifier (a name, not an expression)"
//	"cross(rlm) can only be used as the first argument to a crossing-function call"
//
// so the identifier `main` binds is what `cross` has to be handed, and nothing
// else will type-check. Against gno v1.5.0 the old form failed with the
// uninformative `invalid gno package; type check failed`, which is the whole
// reason this comment names the two rules rather than the symptom.
func generateRunCode(pkgPath, pkgAlias, funcName string, args []string) string {
	var sb strings.Builder
	sb.WriteString("package main\n\n")
	sb.WriteString(fmt.Sprintf("import %q\n\n", pkgPath))
	sb.WriteString("func main(cur realm) {\n")
	sb.WriteString(fmt.Sprintf("\t%s.%s(%s)\n", pkgAlias, funcName, joinRunArgs(args)))
	sb.WriteString("}\n")
	return sb.String()
}

// joinRunArgs is like joinArgs but handles the __cross__ sentinel as a raw token.
func joinRunArgs(args []string) string {
	parts := make([]string, len(args))
	for i, arg := range args {
		if arg == "__cross__" {
			// `cross(cur)`, naming the realm main was handed. A bare `cross`
			// is the pre-v1.5 spelling and no longer type-checks.
			parts[i] = "cross(cur)"
		} else if isNumeric(arg) || arg == "true" || arg == "false" {
			parts[i] = arg
		} else {
			parts[i] = `"` + arg + `"`
		}
	}
	return strings.Join(parts, ", ")
}

// printRunGnokeyCmd prints the gnokey equivalent of a RUN, measured.
//
// It shares gasFlags and keyToken with the CALL path deliberately. Those two were
// fixed on the CALL side alone once already, and this function went on printing a
// hardcoded `-gas-wanted=10000000` and a literal `<key-name>` for another commit:
// the exact two defects, in the exact tool that exists to prevent the first one,
// surviving because the code was copied rather than shared.
func printRunGnokeyCmd(cfg *baseCfg, p *GnoPath, code string, io commands.IO) error {
	remote, err := cfg.resolveRemote(p.Domain)
	if err != nil {
		return err
	}

	io.Println("# Generated code (save to run.gno):")
	io.Println(code)
	io.Println()

	parts := []string{
		"gnokey", "maketx", "run",
		"-broadcast",
		fmt.Sprintf("-chainid=%s", remote.ChainID),
		fmt.Sprintf("-remote=%s", remote.RPC),
	}

	plan, planErr := planRunFromCode(cfg, p, code, io)
	parts = append(parts, gasFlags(cfg, plan, planErr, io)...)

	if cfg.send != "" {
		parts = append(parts, fmt.Sprintf("-send=%s", cfg.send))
	}
	parts = append(parts, keyToken(cfg, io))
	parts = append(parts, "run.gno")

	io.Println(strings.Join(parts, " \\\n  "))
	return nil
}

// planRunFromCode measures the MsgRun that carries this script, building the same
// client and message the broadcast path would.
func planRunFromCode(cfg *baseCfg, p *GnoPath, code string, io commands.IO) (txPlan, error) {
	client, _, err := cfg.signingClient(p.Domain, io)
	if err != nil {
		return txPlan{}, err
	}
	msg, err := cfg.runMsg(client, code)
	if err != nil {
		return txPlan{}, err
	}
	return cfg.planTx(client, runBuilder(msg))
}
