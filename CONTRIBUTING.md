# Contributing to gnopie

The authoritative guide. [`AGENTS.md`](./AGENTS.md) and [`CLAUDE.md`](./CLAUDE.md)
point here.

Before anything else, read the warning block at the top of the
[README](./README.md). This tool signs and broadcasts real transactions, it has
never been audited, and the bar for a change that touches how a number reaches
the chain is correspondingly higher than the size of the diff suggests.

## Setup

```sh
make                 # help
make test-unit       # the pure tests: no chain, no GNOROOT, under a second
make all             # what CI runs: lint + the full suite
make run ARGS="INSPECT gno.land/r/gnoland/blog"
make install         # put gnopie on your PATH
```

`make test-unit` is the one to run in a loop. It covers path parsing, fee
arithmetic, config, the query cache and the generated MsgRun script, and it needs
nothing on disk.

### GNOROOT, and why the full suite needs it

gnopie links `github.com/gnolang/gno` **as a library**, pinned in `go.mod`. The
integration tests boot an in-memory node out of that library and load realms from
a gno source tree's `examples/`, then type-check them with the linked VM. So the
tree on disk has to be at **the tag `go.mod` pins**, not at master.

```sh
make gnoroot         # prints the exact command for the current pin
```

Mismatched, it does not fail cleanly. It fails as:

```
invalid gno package; type check failed
```

which names neither the version nor the line. Without a tree at all the chain
tests **skip**, loudly, and the rest still run.

## Layout

```
main.go              flags, dispatch, the shared query helpers
paths.go             ParsePath: everything the user can type, into a GnoPath
get.go               GET / EVAL / READ / INSPECT, the read-only verbs
source.go            pulling one declaration out of a .gno file, by parsing it
call.go              CALL, and the shared gnokey-command rendering
run.go               RUN, and the generated main.gno
tx.go                the transaction planner: gas and fee, one place
fee.go               the gas-price arithmetic, and why the fee is a ratio
types.go             rendering what qfuncs reports, and the realm interface
discover.go          gnoconnect meta-tag discovery, and its 24h cache
querycache.go        the 1h cache for source and signatures
config.go            $GNOHOME/gnopie/config.toml
scripts/devnode/     a throwaway in-memory chain, for screenshots
scripts/screenshots.sh  regenerates docs/img/ from real output
scripts/termsvg.py   captured text -> SVG
staticcheck.conf     which checks are on, and why two are off
```

## The rules that are not style

### Never print a number the chain was not asked for

This is the whole point of the tool, and it is the thing that has gone wrong
twice.

`--print-gnokey-command` printed a hardcoded `-gas-wanted=10000000`. It was found
and fixed on the `CALL` path. `RUN` kept printing it, along with a literal
`<key-name>` as the final token, because the two functions were **copies** rather
than callers of one thing.

So: gas and fee are settled in exactly one place, `(*baseCfg).planTx` in
`tx.go`, and both verbs call it. If you add a third verb that signs, it calls it
too. A new `fmt.Sprintf("-gas-wanted=%d", ...)` anywhere outside `gasFlags` is a
bug in review.

When the number cannot be had, **say so and omit the flag**. Do not substitute a
plausible one. `TestPrintGnokeyCommand_NoKeyOmitsGasAndSaysWhy` asserts this for
both verbs.

### The fee is a ratio, not an amount

`gas_fee / gas_wanted` is compared against the block gas price by the ante
handler, so a flat fee bounces as a transaction gets bigger, and a flat fee large
enough never to bounce is money burned (it is transferred in full and never
refunded). `fee.go` has the citation and the arithmetic. Anything that sets a fee
without reference to the gas beside it is wrong.

### A test that passes is not evidence until you have seen it fail

Break the thing it covers, confirm it goes red, restore. A test that *cannot*
fail reads exactly like a test that is satisfied. Three of the tests in this repo
were written against a deliberately reintroduced bug for precisely this reason;
`gnokeycmd_test.go` says which.

### Parse, do not scan

`source.go` pulls a declaration out of a file with `go/parser`, not by finding a
line and counting braces. The scanner it replaced had two silent failures: a
brace inside a string literal ran the extraction to the end of the file, and a
comment mentioning `func Helper` matched before the real declaration. Both
printed plausible garbage rather than erroring.

gno is syntactically Go for declarations: measured 2026-09-28, `go/parser` read
1036 of 1036 `.gno` files under a v1.5.0 `examples/` tree. The old scanner
survives only as a fallback for a file that will not parse. Do not extend it.

### Match a VM-generated string on its shape, never on a literal

`vm/qfuncs` reports a type's structure, not its name, so a crossing function's
first parameter arrives as the whole realm interface. There was already a fix for
that: a constant holding the literal prefix the VM emitted at the time. By v1.5.0
it matched nothing, because the VM had added `.seal`, dropped `Coins` and stopped
qualifying `address`. It failed **silently**, and the raw 290-character interface
went on into a README screenshot.

`types.go` parses the method set and decides on a quorum. Adding a case means
adding to `realmMethods` or to `cleanType`, with the real string in
`types_test.go` copied verbatim from actual output, never retyped.

### Table-driven, and named after the claim

`{name: "a comma inside a quoted arg is not a separator"}` is worth more than
`{name: "case 3"}`, because the name is what a failure prints.

### Screenshots are generated, never taken

`docs/img/*.svg` come out of `make screenshots`, which runs the real binary
against a real (throwaway) chain. CI regenerates them and fails if the committed
ones differ. If you change output, regenerate and commit; do not edit an SVG.

## Style

- Errors name the fix, not just the problem, and a second sentence is fine.
  `ST1005` is off for this reason.
- Comments explain **why**, especially why a number is what it is. The code
  already says what it does.
- No em dashes.

## Before you open a pull request

```sh
make all
make screenshots && git diff --stat docs/img/   # if you changed any output
```

CI runs `unit`, `lint`, `test`, `install` and `screenshots`. All five have to be
green. The `test` job checks out gno at the pinned tag itself, so you do not need
one for CI, only locally.

## Bumping the gno pin

When gno cuts a release:

```sh
go get github.com/gnolang/gno@vX.Y.Z && go mod tidy
make gnoroot                       # get a tree at the new tag
GNOROOT=/tmp/gno-vX.Y.Z make all
```

Expect the MsgRun generator to be what breaks. `generateRunCode` in `run.go`
emits an entry point and a cross-call whose spelling the VM has already changed
once; `TestGenerateRunCode` asserts the current one. `preprocess.go` in the gno
tree is where the rules are actually written down.

## Licence

Apache-2.0 OR MIT, at your option, the same as the rest of moul's Go tooling. See
[`COPYRIGHT`](./COPYRIGHT). Contributions are under the same terms; there is no
CLA and no copyright assignment.
