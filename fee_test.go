package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The floor is the ante handler's rule restated: gas_fee >= gas_wanted / 1000,
// rounded up. Rounded up matters, because integer division down would produce a
// fee one ugnot short of the floor for every gas_wanted that is not a multiple of
// 1000, which is almost all of them.
func TestGasFeeFloor(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name      string
		gasWanted int64
		want      int64
	}{
		{"zero is zero", 0, 0},
		{"negative is zero", -1, 0},
		{"one gas still costs one ugnot", 1, 1},
		{"exactly the denominator", 1_000, 1},
		{"one over rounds up", 1_001, 2},
		{"one under rounds up", 999, 1},
		{"the measured mainnet call", 11_732_203, 11_733},
		{"the simulation probe", 100_000_000, 100_000},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, gasFeeFloor(tt.gasWanted))
		})
	}
}

func TestGasFeeFor(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name      string
		gasWanted int64
		marginPct int64
		want      int64
	}{
		{"the floor exactly", 11_732_203, 100, 11_733},
		{"the default margin doubles it", 11_732_203, 200, 23_466},
		{"a margin below the floor is raised to it", 11_732_203, 50, 11_733},
		{"a zero margin is raised to the floor", 11_732_203, 0, 11_733},
		{"a negative margin is raised to the floor", 11_732_203, -100, 11_733},
		{"ten times the floor", 11_732_203, 1000, 117_330},
		{"never zero, however small the transaction", 1, 100, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, gasFeeFor(tt.gasWanted, tt.marginPct))
		})
	}
}

// The property that makes the whole change worth having: whatever the margin, the
// fee gnopie derives clears the floor for the gas it derived it from. A fee that
// does not is a transaction the ante handler rejects before it runs.
func TestGasFeeForAlwaysClearsTheFloor(t *testing.T) {
	t.Parallel()
	for _, gas := range []int64{1, 999, 1_000, 1_001, 100_000, 11_732_203, 100_000_000, 1 << 40} {
		for _, margin := range []int64{-500, 0, 99, 100, 101, 200, 1000} {
			fee := gasFeeFor(gas, margin)
			require.GreaterOrEqual(t, fee, gasFeeFloor(gas),
				"gas=%d margin=%d: fee %d is under the floor %d", gas, margin, fee, gasFeeFloor(gas))
		}
	}
}

// gas_wanted is a ceiling and is refunded; the buffer is therefore free and only
// ever raises. The floor exists so a trivially cheap call is not one rounding
// error from bouncing.
func TestWithBuffer(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name      string
		gasUsed   int64
		bufferPct int64
		want      int64
	}{
		{"the default 20 percent", 10_000_000, 20, 12_000_000},
		{"no buffer at all", 10_000_000, 0, 10_000_000},
		{"a negative buffer does not shrink it", 10_000_000, -50, 10_000_000},
		{"small calls are raised to the floor", 1_000, 20, minGasWanted},
		{"exactly at the floor", 100_000, 0, minGasWanted},
		{"just over the floor keeps its buffer", 100_000, 20, 120_000},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, withBuffer(tt.gasUsed, tt.bufferPct))
		})
	}
}

func TestParseUgnot(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		in      string
		want    int64
		wantErr string
	}{
		{name: "plain", in: "23466ugnot", want: 23466},
		{name: "zero", in: "0ugnot", want: 0},
		{name: "surrounding space", in: "  23466ugnot  ", want: 23466},
		{name: "space before the denom", in: "23466 ugnot", want: 23466},
		{name: "no denom is rejected", in: "23466", wantErr: "must be in ugnot"},
		{name: "GNOT is not ugnot", in: "1gnot", wantErr: "must be in ugnot"},
		{name: "not a number", in: "lotsugnot", wantErr: "invalid syntax"},
		{name: "negative is rejected", in: "-1ugnot", wantErr: "must not be negative"},
		{name: "empty", in: "", wantErr: "must be in ugnot"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseUgnot(tt.in)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestGnotString(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{1, "0.000001"},
		{23_466, "0.023466"},
		{1_000_000, "1"},
		{1_500_000, "1.5"},
		{11_733, "0.011733"},
		{-1_500_000, "-1.5"},
		{123_456_789, "123.456789"},
	} {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, gnotString(tt.in))
		})
	}
}

func TestUgnotString(t *testing.T) {
	t.Parallel()
	require.Equal(t, "23466ugnot", ugnotString(23466))
	require.Equal(t, "0ugnot", ugnotString(0))
}

// Round-trip, because the two are used together: gnopie derives a fee, renders
// it into a gnokey command, and a later invocation may be handed it back as
// --gas-fee.
func TestUgnotRoundTrip(t *testing.T) {
	t.Parallel()
	for _, v := range []int64{0, 1, 11_733, 23_466, 1_000_000, 1 << 40} {
		got, err := parseUgnot(ugnotString(v))
		require.NoError(t, err)
		require.Equal(t, v, got)
	}
}

// The plan line is the last thing printed before money moves, so it gets a test
// of its own: it said "4032ugnot ugnot" for one commit, because the denomination
// was both in the value and in the format string.
func TestTxPlanDescribe(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		plan txPlan
		want []string
		deny []string
	}{
		{
			name: "measured and derived",
			plan: txPlan{GasWanted: 2_015_118, GasFee: 4032, Measured: true},
			want: []string{"2015118", "(measured)", "4032ugnot", "0.004032 GNOT", "(derived)"},
			deny: []string{"ugnot ugnot", "--gas-wanted"},
		},
		{
			name: "both supplied",
			plan: txPlan{GasWanted: 10, GasFee: 1, FeeGiven: true},
			want: []string{"from --gas-wanted", "from --gas-fee"},
			deny: []string{"ugnot ugnot", "measured", "derived"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.plan.Describe()
			for _, w := range tt.want {
				require.Contains(t, got, w)
			}
			for _, d := range tt.deny {
				require.NotContains(t, got, d)
			}
		})
	}
}
