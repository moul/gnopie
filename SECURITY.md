# Security

## First, the honest framing

**gnopie has never been audited.** Not by a firm, not by a third party, not
informally by anyone qualified. Much of it was written with AI assistance. It is
one person's tool, published because copyleft wants the source published and
because it is useful, not because it is ready to be trusted.

It signs and broadcasts real transactions on a real chain. Those are
irreversible. Treat it accordingly: `--dry-run` and `-print` exist so you never
have to take its word for one.

## What it does with your keys

- gnopie reads the **gnokey keybase** at `$GNOHOME` (default `~/.config/gno`).
  It does not create, store or transmit keys. Key management is gnokey's job.
- It asks for your passphrase to sign, and to **simulate**: gas estimation runs
  the chain's real ante handler, which verifies the signature, so measuring
  needs an unlocked key even though nothing is broadcast.
- `-insecure-password-stdin` reads the passphrase from stdin, for scripts. It is
  called insecure because it is: the passphrase lands in your shell history, your
  process list or your CI logs depending on how you feed it.
- gnopie never writes a passphrase or a mnemonic anywhere.

## What it sends over the network, and to whom

- **Discovery** fetches `https://<domain>/` and reads its
  `<meta name="gnoconnect:*">` tags to learn the RPC endpoint and chain ID.
  **The domain you name decides where your transaction goes.** A domain you do
  not control, or a hijacked one, can point gnopie at an RPC of its choosing. The
  result is cached for 24h under `$GNOHOME/gnopie/cache/`.
- Everything else is ABCI queries and transaction broadcasts to that endpoint.
- No telemetry, no analytics, no phone-home.

## The things most likely to bite you

1. **Version drift.** gnopie links gno as a library and is pinned to one version.
   A chain newer than the pin can make it **silently wrong** rather than merely
   broken. `gnopie version` prints which gno it linked.
2. **Argument handling.** Arguments are parsed from a string and forwarded to the
   VM. A quoted argument reaches the chain as a string and a bare number as a
   number; if you are unsure which happened, `--dry-run` and `-print` show you
   before anything is signed.
3. **The generated MsgRun script.** `RUN` composes Gno source and executes it on
   chain. `--dry-run` prints exactly what would run. Read it.

## Reporting something

Open a **private** vulnerability report through GitHub Security Advisories:
<https://github.com/moul/gnopie/security/advisories/new>. That is the preferred
channel and it is enabled on this repository.

If the issue is in **gno itself** rather than in gnopie, report it to
<https://github.com/gnolang/gno/security/policy> instead. gnopie is a thin client
over gno's own packages, so most things that look like a chain bug are one.

For anything low-risk, a normal public issue is fine and faster.

### What to expect

This is unfunded personal tooling maintained by one person. There is no SLA and
no bounty. I will acknowledge a report when I see it, and fix what is fixable.
If you need a guarantee, you need a different tool.

## Supported versions

Only `main`, and only against the gno version in its `go.mod`. There are no
backports.
