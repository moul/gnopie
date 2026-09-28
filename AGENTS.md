# Agent instructions for gnopie

Read [`CONTRIBUTING.md`](./CONTRIBUTING.md). It is the authoritative guide and
this file only repeats what an agent gets wrong most often.

## Loop

```sh
make test-unit    # under a second, no chain, no GNOROOT. Use this while working.
make all          # lint + full suite. Before you claim done.
```

The full suite needs `GNOROOT` at the tag `go.mod` pins. `make gnoroot` prints
the command. Without it the chain tests **skip** rather than fail, so a green
`go test ./...` does not by itself mean the chain paths ran. Check for `SKIP`.

## The rule that matters more than the rest

**Never emit a gas or fee number the chain was not asked for.** Not in code, not
in a comment, not in an example in a doc. That defect has shipped twice here, and
the second time it survived the fix because the two code paths were copies.

Gas and fee are settled in `(*baseCfg).planTx` (`tx.go`) and nowhere else. If you
add a verb that signs, call it. If you find yourself writing `-gas-wanted=` as a
literal, stop.

## Verifying a test

A passing test proves nothing until you have watched it fail. Break the code it
covers, run it, see red, restore. State in the pull request that you did.

## Do not

- Do not edit `docs/img/*.svg`. Run `make screenshots`.
- Do not hand-write a version string. `gnopie version` reads build info.
- Do not relicense anything, or add an SPDX header that disagrees with
  [`COPYRIGHT.md`](./COPYRIGHT.md). The licence is inherited from gno, not chosen.
- Do not add a dependency without saying why in the pull request. The tree is
  already large because gno is in it.
