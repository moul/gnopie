# Copyright and licence

Copyright 2026 Manfred Touron and other gnopie contributors.
Portions copyright 2025 NewTendermint, LLC.

gnopie is licensed under the **GNO Network General Public License**, version 6 or
later, the full text of which is in [`LICENSE.md`](./LICENSE.md). That is a fork
of the GNU Affero General Public License 3, with additional attribution terms.

## Why this licence and not Apache or MIT

Not a choice, an inheritance, and worth stating plainly because the rest of
moul's Go tooling is dual Apache-2.0 / MIT and this one cannot be.

1. **It started as gno.** gnopie was extracted from
   [gnolang/gno#5444](https://github.com/gnolang/gno/pull/5444), a draft against
   that repository's `contribs/` directory. The code is a derivative of a work
   under the GNO NGPL.
2. **It links gno as a library.** `go.mod` requires `github.com/gnolang/gno`, and
   gnopie imports the VM, the gno.land SDK and the tm2 client packages directly.
   An AGPL-derived copyleft reaches a work that links it.

Relicensing would need the agreement of NewTendermint, LLC, which nobody has
asked for and which this tool does not need. Copyleft also wants the source
published, which is where it is.

## What that means if you use it

- You may use, study, modify and redistribute it.
- If you distribute it, or run a modified version as a **network service**, you
  have to offer the corresponding source under the same licence. That is the
  Affero clause, and it is the part people are usually surprised by.
- The additional attribution terms in §7 apply: a user interface of a covered
  work, or of something that derives functionality from it, has to display an
  attribution notice linking to gno.land. Read the section; it is short and it is
  specific.

## Third-party code

The dependency tree is gno's, which is itself split: `gnolang/gno` is under the
GNO NGPL, and its `tm2/` subdirectory is Apache-2.0. `go.sum` is the exhaustive
list; `go mod graph` is how to read it.

## Not legal advice

This file is a summary written by a developer. Where it and
[`LICENSE.md`](./LICENSE.md) disagree, `LICENSE.md` is the licence and this is
just prose.
