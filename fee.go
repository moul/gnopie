package main

import (
	"fmt"
	"strconv"
	"strings"
)

// Gas pricing on gno.land, and why gnopie sizes the fee instead of taking one.
//
// The minimum fee is a RATIO of gas_wanted, not a flat amount.
// EnsureSufficientMempoolFees (tm2/pkg/sdk/auth/ante.go, v1.5.0 line 507) builds
// a GasPrice{Gas: gas_wanted, Price: gas_fee} and compares it against the block
// gas price with IsGTE, which cross-multiplies. The rule that falls out is:
//
//	gas_fee / gas_wanted  >=  block gas price
//
// Two consequences, and gnopie used to be on the wrong side of both.
//
//  1. A FLAT fee bounces as the transaction gets bigger. gas_wanted is measured,
//     so it grows with what the code does; a fee that does not grow with it makes
//     the ratio fall until the ante handler rejects it. This is the same failure
//     the README describes for gas_wanted, one level up.
//
//  2. A flat fee that is large enough never to bounce is money burned. The fee is
//     not a ceiling like gas_wanted: DeductFees (same file, line 476) calls
//     SendCoinsUnrestricted for the full amount and nothing refunds the
//     difference. gnopie's old default of 1000000ugnot is 1 GNOT on every
//     transaction. Against the floor for a measured 11,732,203 gas call (11,733
//     ugnot) that is 85x the required fee, paid in full, every time.
//
// So the fee is derived from the measured gas, with a margin, and an explicit
// --gas-fee still wins.
const (
	// gasPriceDen is the ugnot of fee required per this much gas: the floor is
	// gas_wanted / gasPriceDen, rounded up.
	gasPriceDen = 1000

	// defaultFeeMargin is the headroom over the floor, in percent. 200 means
	// twice the floor.
	//
	// Small on purpose. Every percent of margin is spent and never returned, so
	// the only thing the margin buys is surviving the block gas price moving
	// under a transaction already in flight. Doubling the floor covers that and
	// still costs ~0.023 GNOT on a call where the old flat default cost 1.
	defaultFeeMargin = 200
)

// gasFeeFloor is the smallest fee the ante handler accepts for this gas_wanted.
func gasFeeFloor(gasWanted int64) int64 {
	if gasWanted <= 0 {
		return 0
	}
	return (gasWanted + gasPriceDen - 1) / gasPriceDen
}

// gasFeeFor sizes a fee for a measured gas_wanted at a margin over the floor.
//
// marginPct is a percentage OF THE FLOOR: 100 is the floor exactly, 200 is twice
// it. Anything under 100 is raised to 100, because a fee below the floor does not
// buy a cheaper transaction, it buys a rejected one.
func gasFeeFor(gasWanted, marginPct int64) int64 {
	if marginPct < 100 {
		marginPct = 100
	}
	floor := gasFeeFloor(gasWanted)
	fee := floor * marginPct / 100
	if fee < 1 {
		fee = 1
	}
	return fee
}

// ugnotString renders an amount as the coin string gnokey and the SDK expect.
func ugnotString(amount int64) string {
	return strconv.FormatInt(amount, 10) + "ugnot"
}

// gnotString renders ugnot as a human GNOT amount, trimming trailing zeros, for
// the one line that tells you what a transaction is about to cost.
func gnotString(ugnot int64) string {
	neg := ""
	if ugnot < 0 {
		neg, ugnot = "-", -ugnot
	}
	whole, frac := ugnot/1_000_000, ugnot%1_000_000
	if frac == 0 {
		return neg + strconv.FormatInt(whole, 10)
	}
	return neg + strconv.FormatInt(whole, 10) + "." +
		strings.TrimRight(fmt.Sprintf("%06d", frac), "0")
}

// parseUgnot accepts "1000000ugnot" and returns the amount.
//
// Deliberately narrow: gnopie only ever needs to read back a fee it or the user
// wrote as ugnot, and a permissive parser here would silently accept a denom the
// chain will reject later, which is the kind of error this tool exists to move
// earlier rather than later.
func parseUgnot(s string) (int64, error) {
	rest, ok := strings.CutSuffix(strings.TrimSpace(s), "ugnot")
	if !ok {
		return 0, fmt.Errorf("fee %q must be in ugnot (for example 23466ugnot)", s)
	}
	v, err := strconv.ParseInt(strings.TrimSpace(rest), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("fee %q: %w", s, err)
	}
	if v < 0 {
		return 0, fmt.Errorf("fee %q must not be negative", s)
	}
	return v, nil
}
