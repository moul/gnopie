#!/usr/bin/env bash
#
# Regenerate the terminal images in docs/img/ from REAL command output.
#
# Every image in the README comes from running gnopie here, against a throwaway
# in-memory chain, so none of them can quietly stop matching what the tool
# prints. A screenshot taken by hand rots the first time an output line changes
# and nobody notices.
#
# Offline on purpose. A capture taken against mainnet would bake in a block
# height, an account balance and a gas number that were true for one afternoon,
# and it would need a funded key to show CALL at all. scripts/devnode gives us a
# chain with a funded test account and a counter realm, which is enough to show
# every verb including the ones that sign.
#
# usage: scripts/screenshots.sh [outdir]
#        GNOROOT=/path/to/gno scripts/screenshots.sh

set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/.." && pwd)"
out="${1:-$root/docs/img}"
mkdir -p "$out"

: "${GNOROOT:=$HOME/p/gh/gnolang/gno}"
export GNOROOT
if [ ! -d "$GNOROOT/examples" ]; then
  echo "GNOROOT=$GNOROOT has no examples/." >&2
  echo "Point it at a gnolang/gno checkout at the tag go.mod pins (see README, Testing)." >&2
  exit 1
fi

tmp="$(mktemp -d)"
trap 'kill "${node_pid:-}" 2>/dev/null || true; rm -rf "$tmp"' EXIT

bin="$tmp/gnopie"
(cd "$root" && go build -o "$bin" .)
# Built rather than `go run`: `go run` execs the compiled binary as a child, so
# killing the go process leaves the node holding its port.
node_bin="$tmp/devnode"
(cd "$root" && go build -o "$node_bin" ./scripts/devnode)

home="$tmp/home"
mkdir -p "$home"
"$node_bin" -pkgs gno.land/r/demo/counter -home "$home" > "$tmp/node.txt" 2>/dev/null &
node_pid=$!

# Wait for the address line rather than sleeping a guess.
for _ in $(seq 1 300); do
  [ -s "$tmp/node.txt" ] && break
  sleep 0.2
done
[ -s "$tmp/node.txt" ] || { echo "devnode never reported an address" >&2; exit 1; }
grep -q '^rpc=' "$tmp/node.txt" || { echo "devnode said: $(cat "$tmp/node.txt")" >&2; exit 1; }

# The keybase devnode seeded has an empty password, fed on stdin, because a
# capture cannot answer a prompt.
gnopie() { printf '\n' | "$bin" -home "$home" -insecure-password-stdin "$@"; }
svg() { python3 "$here/termsvg.py" "$1" > "$out/$2"; }

# 1. the default verb, on a realm. The first thing anybody types.
{
  echo '$ gnopie gno.land/r/demo/counter'
  gnopie gno.land/r/demo/counter 2>&1
  echo ''
  echo '$ gnopie '"'"'gno.land/r/demo/counter.Render("")'"'"''
  gnopie 'gno.land/r/demo/counter.Render("")' 2>&1
} | svg "gnopie: read a realm, evaluate a function" get.svg

# 2. INSPECT: the files, the functions and their signatures, in one call.
{ echo '$ gnopie INSPECT gno.land/r/demo/counter'; gnopie INSPECT gno.land/r/demo/counter 2>&1; } |
  svg "gnopie INSPECT: what is in there" inspect.svg

# 3. READ: the source of one function, without cloning anything.
{ echo '$ gnopie READ gno.land/r/demo/counter.Increment'; gnopie READ gno.land/r/demo/counter.Increment 2>&1; } |
  svg "gnopie READ: the source, from the chain" read.svg

# 4. THE one. --print-gnokey-command, with a measured gas and a fee derived
#    from it. This is the picture the whole tool is for.
{
  echo '$ gnopie CALL --print-gnokey-command '"'"'gno.land/r/demo/counter.Increment()'"'"''
  gnopie CALL --print-gnokey-command 'gno.land/r/demo/counter.Increment()' 2>&1
} | svg "gnopie CALL --print-gnokey-command: measured, not guessed" gnokey.svg

# 5. the broadcast, and the state change it caused. Last, because it is the
#    only capture that mutates the chain.
{
  echo '$ gnopie gno.land/r/demo/counter'
  gnopie gno.land/r/demo/counter 2>&1
  echo ''
  echo '$ gnopie CALL '"'"'gno.land/r/demo/counter.Increment()'"'"''
  gnopie CALL 'gno.land/r/demo/counter.Increment()' 2>&1
  echo ''
  echo '$ gnopie gno.land/r/demo/counter'
  gnopie gno.land/r/demo/counter 2>&1
} | svg "gnopie CALL: measure, sign, broadcast" call.svg

# 6. the whole surface.
#
# GNOHOME is pinned to a neutral literal for this one capture, because --help
# prints the DEFAULT home, which is computed from the environment. Without it the
# committed SVG carries whoever ran it last: their username in a file in the
# README, and a CI staleness check that can never pass because the runner's home
# is not theirs.
#
# `--help` exits non-zero, as the commands package does for every help request,
# so it is explicitly tolerated rather than tripping `set -e`.
{
  echo '$ gnopie --help'
  GNOHOME=/home/user/.config/gno "$bin" --help 2>&1 || true
} | svg "gnopie --help" help.svg

ls -1 "$out"
