package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gnolang/gno/gno.land/pkg/gnoclient"
	"github.com/gnolang/gno/tm2/pkg/commands"
)

// execGet is the default verb — smart dispatch:
//   - PathCall → EVAL (evaluate function)
//   - PathSymbol → READ (read variable or source)
//   - PathNetwork/PathNamespace/PathPackage → INSPECT
func execGet(ctx context.Context, cfg *baseCfg, expr string, io commands.IO) error {
	p, err := ParsePath(expr)
	if err != nil {
		return fmt.Errorf("parsing: %w", err)
	}

	cfg.debugf(io, "parse           kind=%d domain=%s pkg=%s sym=%s", p.Kind, p.Domain, p.PkgPath, p.Symbol)

	// Handle gnoweb modifiers
	if p.RenderPath == "$source" {
		cfg.debugf(io, "route           → READ ($source)")
		if p.File != "" {
			// $source&file=admin.gno → read specific file
			return readFile(cfg, p, io)
		}
		return execRead(ctx, cfg, expr, io)
	}
	if p.RenderPath == "$help" || p.RenderPath == "$funcs" {
		cfg.debugf(io, "route           → INSPECT ($help)")
		if p.Symbol != "" {
			// $help#func-Name → inspect specific function
			return readFuncSignature(cfg, p, io)
		}
		return execInspect(ctx, cfg, expr, io)
	}

	switch p.Kind {
	case PathCall:
		cfg.debugf(io, "route           → EVAL")
		return execEval(ctx, cfg, expr, io)
	case PathSymbol:
		cfg.debugf(io, "route           → READ")
		return execRead(ctx, cfg, expr, io)
	case PathFile:
		cfg.debugf(io, "route           → READ (file)")
		return readFile(cfg, p, io)
	case PathAddress:
		cfg.debugf(io, "route           → INSPECT (address)")
		return inspectAddress(cfg, p, io)
	case PathUser:
		cfg.debugf(io, "route           → user lookup")
		return inspectUser(cfg, p, io)
	case PathPackage:
		// Default for packages: call Render("") like gnoweb
		cfg.debugf(io, "route           → Render")
		return getRender(cfg, p, io)
	default:
		cfg.debugf(io, "route           → INSPECT")
		return execInspect(ctx, cfg, expr, io)
	}
}

// execEval evaluates a read-only function call via qeval.
func execEval(_ context.Context, cfg *baseCfg, expr string, io commands.IO) error {
	p, err := ParsePath(expr)
	if err != nil {
		return fmt.Errorf("parsing: %w", err)
	}

	if p.Kind != PathCall && p.Kind != PathSymbol {
		return fmt.Errorf("EVAL expects a function call like gno.land/r/foo/bar.Func(...)")
	}

	c, _, err := cfg.queryClient(p.Domain)
	if err != nil {
		return err
	}

	// No `cross` is injected here, on purpose, and this is the third time this
	// tool has had to learn the same lesson about spelling a VM builtin by hand.
	//
	// It used to prepend a bare `cross` for crossing functions. Against gno
	// v1.5.0 that fails outright:
	//
	//	use of builtin cross not in function call
	//	--- preprocess stack ---
	//
	// so EVAL was broken for EVERY crossing function, which is most of the
	// interesting ones. It went unnoticed because the test asserted only that a
	// DIFFERENT error ("missing realm argument") was absent, and that stayed true
	// while the feature did not work at all.
	//
	// The chain does it itself: QueryEval calls m.MaybeInjectCurForEval(xx)
	// (gno.land/pkg/sdk/vm/keeper.go), which prepends `.cur` when the parsed
	// expression calls a crossing function in that package. Injecting anything
	// here is fighting it. Verified 2026-09-28 against v1.5.0: with this removed,
	// `EVAL counter.Increment()` returns `(1 int)` where it previously errored.
	var qevalExpr string
	if p.Kind == PathCall {
		qevalExpr = p.Symbol + "(" + joinArgs(p.Args) + ")"
	} else {
		qevalExpr = p.Symbol
	}

	result, _, err := c.QEval(p.PkgPath, qevalExpr)
	if err != nil {
		return fmt.Errorf("eval: %w", err)
	}

	if cfg.jsonOut {
		return outputJSON(io, map[string]any{
			"pkg_path":   p.PkgPath,
			"expression": qevalExpr,
			"result":     result,
		})
	}
	io.Println(result)
	return nil
}

// execRead reads a variable value or source code.
func execRead(_ context.Context, cfg *baseCfg, expr string, io commands.IO) error {
	p, err := ParsePath(expr)
	if err != nil {
		return fmt.Errorf("parsing: %w", err)
	}

	switch p.Kind {
	case PathFile:
		return readFile(cfg, p, io)

	case PathSymbol:
		if p.IsPublic() {
			return readSource(cfg, p, io)
		}
		c, _, err := cfg.queryClient(p.Domain)
		if err != nil {
			return err
		}
		result, _, err := c.QEval(p.PkgPath, p.Symbol)
		if err != nil {
			return fmt.Errorf("reading %s.%s: %w", p.PkgPath, p.Symbol, err)
		}
		if cfg.jsonOut {
			return outputJSON(io, map[string]any{
				"pkg_path": p.PkgPath, "symbol": p.Symbol, "value": result,
			})
		}
		io.Println(result)
		return nil

	case PathPackage:
		qc, _, err := cfg.queryClient(p.Domain)
		if err != nil {
			return err
		}
		fileList, err := queryFileC(qc, cfg, p.PkgPath)
		if err != nil {
			return err
		}
		if cfg.jsonOut {
			return outputJSON(io, splitLines(fileList))
		}
		io.Println(fileList)
		return nil

	case PathNamespace:
		qc, _, err := cfg.queryClient(p.Domain)
		if err != nil {
			return err
		}
		result, err := queryPaths(qc, p.PkgPath)
		if err != nil {
			return err
		}
		if cfg.jsonOut {
			return outputJSON(io, splitLines(result))
		}
		io.Println(result)
		return nil

	default:
		return fmt.Errorf("READ expects a symbol, package, or namespace path")
	}
}

func readSource(cfg *baseCfg, p *GnoPath, io commands.IO) error {
	c, _, err := cfg.queryClient(p.Domain)
	if err != nil {
		return err
	}
	fileList, err := queryFileC(c, cfg, p.PkgPath)
	if err != nil {
		return err
	}
	files := splitLines(fileList)
	cfg.debugf(io, "search          %q in %d files", p.Symbol, len(files))
	for _, fname := range files {
		if !strings.HasSuffix(fname, ".gno") || strings.HasSuffix(fname, "_test.gno") {
			continue
		}
		source, err := queryFileC(c, cfg, p.PkgPath+"/"+fname)
		if err != nil {
			continue
		}
		decl := extractDecl(source, p.Symbol)
		if decl != "" {
			cfg.debugf(io, "found           %s in %s", p.Symbol, fname)
			if cfg.jsonOut {
				return outputJSON(io, map[string]any{
					"pkg_path": p.PkgPath, "symbol": p.Symbol,
					"file": fname, "source": decl,
				})
			}
			io.Printfln("// %s/%s", p.PkgPath, fname)
			io.Println(decl)
			return nil
		}
	}
	return fmt.Errorf("symbol %q not found in %s", p.Symbol, p.PkgPath)
}

// execInspect provides detailed inspection of any gno resource.
func execInspect(_ context.Context, cfg *baseCfg, expr string, io commands.IO) error {
	p, err := ParsePath(expr)
	if err != nil {
		return fmt.Errorf("parsing: %w", err)
	}

	switch p.Kind {
	case PathNetwork:
		return inspectNetwork(cfg, p, io)
	case PathNamespace:
		return inspectNamespace(cfg, p, io)
	case PathPackage:
		return inspectPackage(cfg, p, io)
	case PathSymbol:
		return inspectSymbol(cfg, p, io)
	case PathAddress:
		return inspectAddress(cfg, p, io)
	case PathCall:
		return execEval(context.Background(), cfg, expr, io)
	default:
		return fmt.Errorf("don't know how to inspect %q", expr)
	}
}

func inspectNetwork(cfg *baseCfg, p *GnoPath, io commands.IO) error {
	remote, err := cfg.resolveRemote(p.Domain)
	if err != nil {
		return err
	}
	c, _, err := cfg.queryClient(p.Domain)
	if err != nil {
		return err
	}
	height, _ := c.LatestBlockHeight()
	ver, _, _ := c.QueryAppVersion()

	if cfg.jsonOut {
		return outputJSON(io, map[string]any{
			"domain": p.Domain, "rpc": remote.RPC, "chain_id": remote.ChainID,
			"indexer": remote.Indexer, "block_height": height, "app_version": ver,
		})
	}
	io.Printfln("Network: %s", p.Domain)
	io.Printfln("  RPC:          %s", remote.RPC)
	io.Printfln("  Chain ID:     %s", remote.ChainID)
	if remote.Indexer != "" {
		io.Printfln("  Indexer:      %s", remote.Indexer)
	}
	io.Printfln("  Block height: %d", height)
	if ver != "" {
		io.Printfln("  App version:  %s", ver)
	}
	return nil
}

func inspectNamespace(cfg *baseCfg, p *GnoPath, io commands.IO) error {
	c, _, err := cfg.queryClient(p.Domain)
	if err != nil {
		return err
	}
	result, err := queryPaths(c, p.PkgPath)
	if err != nil {
		return err
	}
	paths := splitLines(result)
	if cfg.jsonOut {
		return outputJSON(io, map[string]any{"path": p.PkgPath, "packages": paths, "count": len(paths)})
	}
	io.Printfln("Namespace: %s (%d packages)", p.PkgPath, len(paths))
	for _, path := range paths {
		io.Printfln("  %s", path)
	}
	return nil
}

func inspectPackage(cfg *baseCfg, p *GnoPath, io commands.IO) error {
	c, _, err := cfg.queryClient(p.Domain)
	if err != nil {
		return err
	}

	funcsJSON, _ := queryFuncsC(c, cfg, p.PkgPath)
	fileList, _ := queryFileC(c, cfg, p.PkgPath)
	files := splitLines(fileList)
	storage, _ := queryStorage(c, p.PkgPath)

	// Extract vars and consts from source files
	var vars, consts []string
	for _, fname := range files {
		if !strings.HasSuffix(fname, ".gno") || strings.HasSuffix(fname, "_test.gno") {
			continue
		}
		source, err := queryFileC(c, cfg, p.PkgPath+"/"+fname)
		if err != nil {
			continue
		}
		v, cn := extractVarsConsts(source)
		vars = append(vars, v...)
		consts = append(consts, cn...)
	}

	if cfg.jsonOut {
		m := map[string]any{"pkg_path": p.PkgPath}
		if funcsJSON != "" {
			m["functions_raw"] = funcsJSON
		}
		if len(vars) > 0 {
			m["vars"] = vars
		}
		if len(consts) > 0 {
			m["consts"] = consts
		}
		if storage != "" {
			m["storage"] = storage
		}
		if cfg.all {
			m["files"] = files
		}
		return outputJSON(io, m)
	}

	io.Printfln("Realm: %s", p.PkgPath)
	if storage != "" {
		io.Printfln("Storage: %s", storage)
	}
	io.Println()

	if funcsJSON != "" && funcsJSON != "null" && funcsJSON != "[]" {
		io.Println("Functions:")
		formatFuncs(io, funcsJSON)
	}

	if len(vars) > 0 {
		io.Println()
		io.Println("Variables:")
		for _, v := range vars {
			io.Printfln("  %s", v)
		}
	}

	if len(consts) > 0 {
		io.Println()
		io.Println("Constants:")
		for _, cn := range consts {
			io.Printfln("  %s", cn)
		}
	}

	if cfg.all && len(files) > 0 {
		io.Println()
		io.Println("Files:")
		for _, f := range files {
			io.Printfln("  %s", f)
		}
	}

	return nil
}

// extractVarsConsts extracts top-level var and const declarations from source.
func extractVarsConsts(source string) (vars, consts []string) {
	lines := strings.Split(source, "\n")
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])

		if strings.HasPrefix(trimmed, "var ") && !strings.HasPrefix(trimmed, "var (") {
			vars = append(vars, strings.TrimPrefix(trimmed, "var "))
			continue
		}
		if strings.HasPrefix(trimmed, "const ") && !strings.HasPrefix(trimmed, "const (") {
			consts = append(consts, strings.TrimPrefix(trimmed, "const "))
			continue
		}
		if trimmed == "var (" {
			for i++; i < len(lines); i++ {
				inner := strings.TrimSpace(lines[i])
				if inner == ")" {
					break
				}
				if inner != "" && !strings.HasPrefix(inner, "//") {
					vars = append(vars, inner)
				}
			}
			continue
		}
		if trimmed == "const (" {
			for i++; i < len(lines); i++ {
				inner := strings.TrimSpace(lines[i])
				if inner == ")" {
					break
				}
				if inner != "" && !strings.HasPrefix(inner, "//") {
					consts = append(consts, inner)
				}
			}
			continue
		}
	}
	return
}

func inspectSymbol(cfg *baseCfg, p *GnoPath, io commands.IO) error {
	c, _, err := cfg.queryClient(p.Domain)
	if err != nil {
		return err
	}
	result, _, err := c.QEval(p.PkgPath, p.Symbol)
	if err != nil {
		return fmt.Errorf("inspecting %s.%s: %w", p.PkgPath, p.Symbol, err)
	}
	if cfg.jsonOut {
		return outputJSON(io, map[string]any{"pkg_path": p.PkgPath, "symbol": p.Symbol, "value": result})
	}
	io.Printfln("%s.%s = %s", p.PkgPath, p.Symbol, result)
	return nil
}

// getRender calls Render() on a realm — the default GET behavior for packages.
func getRender(cfg *baseCfg, p *GnoPath, io commands.IO) error {
	c, _, err := cfg.queryClient(p.Domain)
	if err != nil {
		return err
	}

	renderPath := p.RenderPath
	if renderPath == "" || strings.HasPrefix(renderPath, "$") {
		renderPath = ""
	}

	cfg.debugf(io, "render          %s:%s", p.PkgPath, renderPath)
	result, _, err := c.Render(p.PkgPath, renderPath)
	if err != nil {
		// If Render fails, fall back to inspect
		cfg.debugf(io, "fallback        Render failed, using INSPECT")
		return inspectPackage(cfg, p, io)
	}

	if cfg.jsonOut {
		return outputJSON(io, map[string]any{
			"pkg_path":    p.PkgPath,
			"render_path": renderPath,
			"result":      result,
		})
	}
	io.Println(result)
	return nil
}

// readFile fetches a specific file from a package.
func readFile(cfg *baseCfg, p *GnoPath, io commands.IO) error {
	c, _, err := cfg.queryClient(p.Domain)
	if err != nil {
		return err
	}
	filePath := p.PkgPath + "/" + p.File
	source, err := queryFileC(c, cfg, filePath)
	if err != nil {
		return fmt.Errorf("reading file: %w", err)
	}
	if cfg.jsonOut {
		return outputJSON(io, map[string]any{
			"pkg_path": p.PkgPath, "file": p.File, "source": source,
		})
	}
	io.Println(source)
	return nil
}

// readFuncSignature shows a specific function's signature from qfuncs.
func readFuncSignature(cfg *baseCfg, p *GnoPath, io commands.IO) error {
	c, _, err := cfg.queryClient(p.Domain)
	if err != nil {
		return err
	}
	funcsJSON, err := queryFuncsC(c, cfg, p.PkgPath)
	if err != nil {
		return fmt.Errorf("querying functions: %w", err)
	}

	type nt struct {
		Name string `json:"Name"`
		Type string `json:"Type"`
	}
	type fs struct {
		FuncName string `json:"FuncName"`
		Params   []nt   `json:"Params"`
		Results  []nt   `json:"Results"`
	}
	var sigs []fs
	if err := json.Unmarshal([]byte(funcsJSON), &sigs); err != nil {
		return fmt.Errorf("parsing functions: %w", err)
	}

	for _, sig := range sigs {
		if sig.FuncName != p.Symbol {
			continue
		}
		var params, results []string
		for _, param := range sig.Params {
			if param.Name != "" {
				params = append(params, param.Name+" "+param.Type)
			} else {
				params = append(params, param.Type)
			}
		}
		for _, r := range sig.Results {
			if r.Name != "" {
				results = append(results, r.Name+" "+r.Type)
			} else {
				results = append(results, r.Type)
			}
		}
		line := fmt.Sprintf("func %s(%s)", sig.FuncName, strings.Join(params, ", "))
		if len(results) == 1 {
			line += " " + results[0]
		} else if len(results) > 1 {
			line += " (" + strings.Join(results, ", ") + ")"
		}

		if cfg.jsonOut {
			return outputJSON(io, map[string]any{
				"pkg_path": p.PkgPath, "function": line,
			})
		}
		io.Println(line)
		return nil
	}
	return fmt.Errorf("function %q not found in %s", p.Symbol, p.PkgPath)
}

// inspectAddress queries account info for a bech32 address.
func inspectAddress(cfg *baseCfg, p *GnoPath, io commands.IO) error {
	// Use default domain for address queries
	c, remote, err := cfg.queryClient("gno.land")
	if err != nil {
		return err
	}
	_ = remote

	cfg.debugf(io, "account         %s", p.Address)

	// Query account via auth/accounts path
	res, err := c.Query(gnoclient.QueryCfg{
		Path: fmt.Sprintf("auth/accounts/%s", p.Address),
		Data: []byte{},
	})
	if err != nil {
		return fmt.Errorf("querying account: %w", err)
	}

	if cfg.jsonOut {
		return outputJSON(io, map[string]any{
			"address":  p.Address,
			"response": string(res.Response.Data),
		})
	}

	io.Printfln("Address: %s", p.Address)
	if len(res.Response.Data) > 0 {
		io.Println(string(res.Response.Data))
	}
	return nil
}

// inspectUser handles /u/username URLs by querying r/sys/users.
func inspectUser(cfg *baseCfg, p *GnoPath, io commands.IO) error {
	c, _, err := cfg.queryClient(p.Domain)
	if err != nil {
		return err
	}

	username := p.Symbol // stored in Symbol by parser
	cfg.debugf(io, "user            %s", username)

	// Render the user page via r/sys/users
	result, _, err := c.Render("gno.land/r/sys/users", username)
	if err != nil {
		return fmt.Errorf("looking up user: %w", err)
	}

	if cfg.jsonOut {
		return outputJSON(io, map[string]any{
			"username": username,
			"result":   result,
		})
	}
	io.Println(result)
	return nil
}

// isCrossingFunc checks if a function's first parameter is a realm type,
// meaning it's a crossing function that needs `cross` as first arg in qeval.
func isCrossingFunc(client *gnoclient.Client, cfg *baseCfg, pkgPath, funcName string) bool {
	funcsJSON, err := queryFuncsC(client, cfg, pkgPath)
	if err != nil || funcsJSON == "" {
		return false
	}

	type nt struct {
		Name string `json:"Name"`
		Type string `json:"Type"`
	}
	type fs struct {
		FuncName string `json:"FuncName"`
		Params   []nt   `json:"Params"`
	}

	var sigs []fs
	if err := json.Unmarshal([]byte(funcsJSON), &sigs); err != nil {
		return false
	}

	for _, sig := range sigs {
		if sig.FuncName == funcName && len(sig.Params) > 0 {
			// A crossing function takes a realm first. Asked through cleanType
			// so there is ONE answer to "is this the realm type", the one in
			// types.go that matches on the interface's method set.
			//
			// This was strings.Contains(type, "realm"), which is true of any
			// type whose name happens to contain the word: a `myrealm` argument,
			// or a struct with a `realm` field, both read as crossing and got a
			// cross(cur) they cannot accept.
			return cleanType(sig.Params[0].Type) == "realm"
		}
	}
	return false
}
