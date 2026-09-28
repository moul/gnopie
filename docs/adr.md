# ADR: gnopie, an httpie-inspired CLI for gno.land

> Carried over from [gnolang/gno#5444](https://github.com/gnolang/gno/pull/5444),
> the draft this tool was extracted from. Kept as the record of what was decided
> and why; where it describes `contribs/gnopie` and a `replace` directive, read
> the README instead: the module is standalone now and the gno dependency is
> pinned.

## Context

Interacting with gno.land chains currently requires composing `gnokey maketx`
commands by hand: you must know the RPC endpoint, chain ID, gas parameters,
function signatures, and argument types. This friction makes casual exploration
and scripting painful.

`httpie` (the HTTP client) solved a similar problem for REST APIs by providing
a natural verb-based syntax, auto-discovery, and sensible defaults. gnopie
applies the same philosophy to gno.land.

Key design goals:

1. **Zero config for reads.** Auto-discover RPC and chain ID from the domain's
   gnoweb page via `<meta name="gnoconnect:*">` tags, no config file needed
   to browse a realm.
2. **Verb-based ergonomics.** GET, EVAL, READ, INSPECT, CALL, RUN mirror the
   mental model of HTTP verbs applied to on-chain resources.
3. **httpie-style smart dispatch.** The default `GET` verb routes automatically:
   realm paths call `Render`, function expressions go to EVAL, symbol names go
   to READ, network domains go to INSPECT.
4. **Auto-gas.** CALL/RUN simulate first, then broadcast with an estimated gas
   + configurable buffer (default 20%) so users never need to guess gas limits.
5. **gnoweb URL passthrough.** Users can paste any `https://gno.land/...` URL
   directly, gnopie strips the `https://`, modifiers (`$source`, `$help`),
   and fragments (`#func-Name`) to route to the right operation.
6. **Caching.** Network discovery is cached 24h; source file and function
   signature queries are cached 1h. State queries (eval, render, storage) are
   never cached.

## Decision

Add `contribs/gnopie` as a standalone Go module with its own `go.mod`,
following the pattern of other contribs (`gnodev`, `gnofaucet`, etc.).

### Module structure

```
contribs/gnopie/
  main.go        , flag parsing, dispatch, query helpers, type cleaning
  get.go         , GET, EVAL, READ, INSPECT verbs
  call.go        , CALL verb (MsgCall) with gas estimation
  run.go         , RUN verb (MsgRun via generated wrapper package)
  paths.go       , URL/expression parser → GnoPath
  discover.go    , gnoconnect meta-tag discovery + disk cache
  querycache.go  , qfile/qfuncs query cache (1h TTL, file-based)
  config.go      , TOML config (key, gas-buffer)
  cmd_config.go  , `gnopie config {get,set,list}` subcommand
  cmd_version.go , `gnopie version` subcommand
  cmd_completion.go, bash/zsh/fish shell completion
```

### Path parsing

`ParsePath` classifies any expression into one of:
`PathNetwork | PathNamespace | PathPackage | PathSymbol | PathCall | PathFile | PathAddress | PathUser`

This lets `dispatch` and `execGet` route without conditional string matching
spread across the codebase.

### Signing and gas estimation

CALL signs a dummy transaction, sends it to the simulation endpoint, reads
`gas_used`, then adds `gas_used * gasBufferPercent / 100` to get `gas_wanted`.
The 20% default buffer is configurable via `gnopie config set gas-buffer=30`.

### RUN implementation

RUN wraps the target function call in a generated Go package (`main` package
with a single `main()` that calls the target), uploads it as `MsgRun`, and
broadcasts the transaction. This mirrors `gnokey maketx run`.

### Crossing functions

Before sending a `qeval` query, gnopie fetches `vm/qfuncs` for the package and
checks if the first parameter of the function is a `realm` type. If so, it
auto-injects `cross` as the first argument, matching the GnoVM requirement for
cross-realm calls evaluated via qeval.

## Alternatives considered

1. **Extend `gnokey`**, `gnokey` is a key-management and transaction tool. Its
   UX is deliberately explicit. Adding auto-discovery and smart dispatch would
   add complexity without benefiting its core audience. A separate binary keeps
   concerns clean.

2. **Extend `gnodev`**, `gnodev` is a local development tool with hot-reload.
   gnopie targets any network (mainnet, testnet, local), not just dev nodes.

3. **Single-file CLI**, considered, but the logic for path parsing, discovery,
   gas estimation, caching, and signing is substantial enough that splitting
   into focused files improves readability without adding abstraction cost.

4. **Use `go-toml/v2` instead of `go-toml v1`**, the repo already uses
   `go-toml v1` in several places. gnopie follows the same convention to avoid
   adding a new major dependency.

5. **In-process caching (sync.Map)**, rejected because gnopie is a CLI
   invoked once per command. Disk caching persists across invocations and
   avoids redundant network requests for source files that change rarely.

## Consequences

- Users can explore any gno.land realm or package with a single command.
- CALL/RUN require only a key name and password, no need to know RPC,
  chain ID, or gas values manually.
- The `gnopie config set key=<name>` workflow is the only required setup for
  signing operations.
- Source code caching means `gnopie READ` on the same function is instant on
  repeated calls (within 1h). Cache can be cleared by removing
  `$GNOHOME/gnopie/cache/`.
- gnopie does not handle multi-message transactions or batch operations, those
  remain the domain of `gnokey`.

---

# ADR 2: the fee is derived from the measured gas, not taken as a flag

**Date:** 2026-09-28. **Status:** accepted.

## Context

ADR 1 above got half of the problem. It established that `gas_wanted` must be
measured rather than guessed, and that is what the tool became known for. The fee
was left as a flag with a flat default, `-gas-fee=1000000ugnot`.

That is wrong in both directions, because the ante handler does not check the fee
as an amount. `EnsureSufficientMempoolFees` (`tm2/pkg/sdk/auth/ante.go`, v1.5.0
line 507) builds a `GasPrice{Gas: gas_wanted, Price: gas_fee}` and compares it to
the block gas price with `IsGTE`, which cross-multiplies. The rule is a ratio:

```
gas_fee / gas_wanted  >=  block gas price
```

So:

1. **A flat fee bounces on a big transaction.** `gas_wanted` is measured and
   grows with what the code does; a constant fee makes the ratio fall until it is
   rejected. This is the exact failure ADR 1 set out to fix, one level down.
2. **A flat fee is money burned on a small one.** `gas_wanted` is a ceiling and
   the unused part is refunded. The fee is not: `DeductFees` (same file, line
   476) calls `SendCoinsUnrestricted` for the full amount. A measured
   `counter.Increment()` needs 4,032 ugnot against a floor of 2,016. The old
   default charged 1,000,000: **248x**, every time.

## Decision

`gas_wanted` and `gas_fee` are settled together, in one function
(`(*baseCfg).planTx`, `tx.go`), and both verbs call it.

- `gas_wanted` = measured + `gas-buffer` percent (default 20). Free, because it
  is refunded.
- `gas_fee` = the floor for that `gas_wanted`, times `fee-margin` percent
  (default 200). Deliberately small, because it is not refunded. The only thing
  the margin buys is surviving the block gas price moving under a transaction
  already in flight.
- `--gas-fee` still wins when given, including a value below the floor: gnopie
  does not silently override a number somebody typed on purpose.

## Why one function and not two

Because the alternative was tried and failed. The hardcoded `-gas-wanted=10000000`
in `--print-gnokey-command` was found and fixed on the `CALL` path, and `RUN` went
on printing it, plus a literal `<key-name>`, for another commit. The two were
copies. Sharing `planTx`, `gasFlags` and `keyToken` is what makes that class of
drift impossible rather than merely unlikely, and
`gnokeycmd_test.go` asserts both verbs separately so a one-sided fix shows up red.

## Consequences

- A transaction is cheaper by two orders of magnitude in the common case, and
  does not bounce in the rare one.
- `fee-margin` joins `gas-buffer` in the config, with an explicit floor of 100
  percent: a fee under the ante handler's floor is a rejected transaction, not a
  cheaper one.
- The plan is printed before the broadcast, so the number is on screen before the
  money moves.

---

# ADR 3: the test suite does not require a gno checkout

**Date:** 2026-09-28. **Status:** accepted.

## Context

gnopie links gno as a library and its integration tests boot an in-memory node,
loading realms out of a gno source tree's `examples/` and type-checking them with
the linked VM. That tree has to be at the tag `go.mod` pins, and a mismatch fails
as `invalid gno package; type check failed`, naming neither the version nor the
line.

Requiring it for the whole suite meant a contributor could run **nothing** until
they had a 400MB checkout at exactly the right tag. It also meant the suite was
slow enough not to run in a loop: every test built its own node, seventeen of them,
to ask read-only questions of the same genesis.

## Decision

Three changes:

1. **One shared node** for read-only tests, owned by `TestMain`. Only tests that
   broadcast get one of their own, because a committed transaction is visible to
   everything else on the same chain.
2. **A missing GNOROOT skips**, loudly, rather than failing. `-short` does the
   same. The pure tests, roughly 160 assertions over path parsing, fee
   arithmetic, config and the generated script, run in about a tenth of a second
   with nothing on disk.
3. **CI runs both**: a fast `unit` job with no checkout at all, and a `test` job
   that checks gno out at the pinned tag, so nothing is skipped where it counts.

## The trap this walked into first

The shared node was originally built lazily from the first test that asked for
it, using that test's `*testing.T`. `t.TempDir()` is removed when **that** test
returns, so the keybase and the pre-warmed discovery cache vanished under every
test that ran afterwards. The symptom was `file is not available` from queries
that had quietly been re-pointed at the real gno.land, several tests later and
with nothing naming the cause. `TestMain` owns both for this reason.
