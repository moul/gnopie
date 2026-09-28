# gnopie

**httpie, but for gno.land.** One command to read a realm, evaluate a function, or
send a transaction, with the network discovered and **the gas measured instead of
guessed**.

```bash
gnopie gno.land/r/gnoland/blog                       # render a realm
gnopie 'gno.land/r/gnoland/blog.Render("")'          # evaluate a function
gnopie READ gno.land/r/gnoland/blog.ModAddPost       # read one function's source
gnopie INSPECT gno.land/r/gov/dao                    # files and functions
gnopie CALL 'gno.land/r/gnoland/wugnot.Withdraw(102639)'
```

---

## ⚠️ Read this before you point it at a chain

> ### THIS IS UNAUDITED, PERSONAL, EXPERIMENTAL TOOLING.
>
> - **Not audited.** Not by anyone, ever. No security review, no third party, no
>   formal anything. Most of it was written with AI assistance.
> - **It signs and broadcasts real transactions with real money.** `CALL` and `RUN`
>   are irreversible. There is no undo on a chain.
> - **It is not a gno.land project.** Not endorsed by, affiliated with, or supported
>   by gno.land, the Gno core team, or any organisation. It is one person's tool.
> - **No stability promise.** Flags, output and behaviour change without notice or a
>   changelog. Nothing here is a supported interface.
> - **It is pinned to one gno version** and has already broken once when the VM moved
>   underneath it (see [Version pinning](#version-pinning)). A newer chain can make it
>   silently wrong, not merely broken.
> - **Read what it is about to do.** `--dry-run` and `--print-gnokey-command` exist so
>   you never have to take its word for a transaction. Use them.
>
> **Do not use it for anything you cannot afford to lose.** If you want a supported
> tool, use [`gnokey`](https://github.com/gnolang/gno/tree/master/gno.land/cmd/gnokey).

---

## Why it exists

Composing a `gnokey maketx` by hand means knowing the RPC endpoint, the chain ID, the
function signature, the argument types, and the gas. Four of those five you can look
up. **The gas you cannot**, because gno prices a transaction on what the code does:
the same `send` costs 1,238,665 when it has to create the recipient's account and a
fraction of that when the account already exists, and nothing in the command says which
case you are in. The fee is worse, because the ante handler compares
`gas_fee / gas_wanted` against the block gas price, so a flat figure that clears at one
ceiling bounces at a higher one.

So people guess, and the guess is wrong:

```
gas used (11674483) exceeds tx's gas wanted (2000000)
```

`gnopie` simulates first and broadcasts with the measured number plus a buffer. There is
no gas flag to get wrong.

## Install

```bash
git clone git@github.com:moul/gnopie.git && cd gnopie
make install                      # -> $GOBIN/gnopie
gnopie config set key=moul        # default signing key
```

Go 1.25.9 or newer. The module is private, so `go install github.com/moul/gnopie@latest`
needs `GOPRIVATE=github.com/moul/*` and git access.

## Verbs

Inspired by httpie: the verb says what kind of thing you are doing, and the default one
is usually right.

| Verb | What it does |
|---|---|
| **GET** (default) | `Render` for a realm, `EVAL` for a call expression, `READ` for a symbol, `INSPECT` for a domain |
| **EVAL** | evaluate a read-only function call |
| **READ** | source of a function, a file, or the value of a variable |
| **INSPECT** | a network, a realm, or a symbol, in detail |
| **CALL** | sign and broadcast a `MsgCall` |
| **RUN** | generate a script and broadcast a `MsgRun` |

## What it does for you

- **Auto-discovery.** Reads `<meta name="gnoconnect:rpc">` off the domain's gnoweb page.
  No endpoint to configure, no chain ID to remember.
- **Auto-gas.** `CALL` and `RUN` simulate, then broadcast with the measured gas plus a
  buffer (20% by default). This is the feature the whole tool is worth having for.
- **Crossing functions.** `cross(cur)` is injected for realm functions that need it,
  detected from the signature rather than assumed.
- **gnoweb URLs.** Paste `https://gno.land/r/gnoland/blog:p/beta-mainnet` straight in;
  the scheme, the render path, `$source`, `$help` and `#fragments` are all understood.
- **Caching.** Discovery for 24h, source and signature queries for 1h, under
  `$GNOHOME/gnopie/cache/`. State reads (eval, render, storage) are never cached.

## Seeing before signing

```bash
gnopie CALL --dry-run 'gno.land/r/demo/counter.Increment()'
gnopie CALL --print-gnokey-command 'gno.land/r/demo/counter.Increment()'
gnopie --json gno.land/r/gnoland/blog | jq .result
gnopie --debug gno.land/r/gnoland/blog
```

`--print-gnokey-command` is the one to reach for when you do not trust it: it prints the
`gnokey` invocation, measured gas and all, and runs nothing.

## Version pinning

gnopie links gno as a **library**, so it is pinned to one version in `go.mod`
(`github.com/gnolang/gno v1.5.0`) and the chain it talks to may be newer.

This is not hypothetical. The MsgRun generator emitted `func main()` and a bare `cross`,
which was correct when it was written and stopped type-checking when the VM moved to
`func main(cur realm)` and `cross(cur)`. The symptom was
`invalid gno package; type check failed`, which names neither the version nor the line.
`preprocess.go` is where the current rules are written down, and they are worth reading
before blaming your own code.

**When gno cuts a release, bump the pin and run the suite.** That is the maintenance this
tool costs.

## Testing

```bash
make test                                    # GNOROOT defaults to ~/p/gh/gnolang/gno
GNOROOT=/path/to/gno make test
```

101 tests: table-driven CLI cases run twice (fresh and cached) against an in-memory node,
plus stateful `CALL`/`RUN` cases that increment a counter and read the state back.

The suite needs a **gno source tree on disk** because it loads realms out of `examples/`,
and **that tree has to match the version in `go.mod`**. Mismatched, it fails as
`invalid gno package; type check failed` with nothing pointing at the cause:

```bash
git -C /path/to/gno worktree add /tmp/gno-v150 v1.5.0
GNOROOT=/tmp/gno-v150 make test
```

## History

Started as [gnolang/gno#5444](https://github.com/gnolang/gno/pull/5444), a draft against
`contribs/`, opened 2026-04-07 and stale since. Extracted here so it can move at its own
pace rather than waiting on a monorepo review, with the module standalone, the gno
dependency pinned, and the MsgRun generator brought up to the current VM.

Design decisions and the alternatives considered: [`docs/adr.md`](docs/adr.md).

## Licence

Same as the upstream it was extracted from. Personal tooling; no support, no warranty,
no promises.
