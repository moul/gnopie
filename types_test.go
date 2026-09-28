package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// realmV150 is the realm interface EXACTLY as vm/qfuncs reported it for
// gno.land/r/demo/counter.Increment against gno v1.5.0, captured 2026-09-28.
// Copied verbatim rather than paraphrased: the previous attempt at this matched
// a hand-written prefix that had never been checked against real output, and it
// matched nothing.
const realmV150 = "interface {.seal func(); Address func() address; IsCode func() bool; " +
	"IsCurrent func() bool; IsEphemeral func() bool; IsUser func() bool; " +
	"IsUserCall func() bool; IsUserRun func() bool; PkgPath func() string; " +
	"Previous func() realm; String func() string; Sub func(string) realm; " +
	"Subpath func() string}"

// realmPreV150 is the older spelling the dead constant was written against:
// .uverse-qualified, with Coins, without .seal. Kept as a case because a tool
// pinned to one gno version will meet old chains.
const realmPreV150 = "interface {Address func() .uverse.address; Coins func() .uverse.gnocoins; " +
	"IsUser func() bool; PkgPath func() string; Previous func() .uverse.realm; " +
	"String func() string}"

func TestCleanTypeRealmInterface(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		in   string
	}{
		{"the v1.5.0 spelling", realmV150},
		{"the pre-v1.5.0 spelling", realmPreV150},
		// The failure this whole file exists to prevent: the VM adds a method
		// and the match stops working. A quorum survives it; an exact list
		// would not.
		{"with a method the VM has not added yet", strings.Replace(
			realmV150, "Subpath func() string}", "Subpath func() string; Bananas func() int}", 1)},
		{"with a method removed", strings.Replace(
			realmV150, "IsEphemeral func() bool; ", "", 1)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, "realm", cleanType(tt.in))
		})
	}
}

// A realm-shaped interface is identified by a quorum, so dropping one of the
// five identifying methods has to stop it matching. Without this, "quorum"
// would be indistinguishable from "any interface at all".
func TestCleanTypeRejectsNonRealmInterfaces(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		in   string
		want string
	}{
		{
			name: "missing PkgPath is not a realm",
			in:   strings.Replace(realmV150, "PkgPath func() string; ", "", 1),
			want: "interface{Address; IsCode; IsCurrent; IsEphemeral; IsUser; IsUserCall; IsUserRun; Previous; String; Sub; Subpath}",
		},
		{
			name: "missing Address is not a realm",
			in:   strings.Replace(realmV150, "Address func() address; ", "", 1),
			want: "interface{IsCode; IsCurrent; IsEphemeral; IsUser; IsUserCall; IsUserRun; PkgPath; Previous; String; Sub; Subpath}",
		},
		{
			name: "an unrelated interface keeps its method names",
			in:   "interface {Read func([]byte) (int, error); Close func() error}",
			want: "interface{Read; Close}",
		},
		{name: "the error interface", in: "interface {Error func() string}", want: "error"},
		{name: "the empty interface", in: "interface {}", want: "any"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, cleanType(tt.in))
		})
	}
}

func TestCleanTypePlainTypes(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ in, want string }{
		{"string", "string"},
		{"int", "int"},
		{"[]byte", "[]byte"},
		{".uverse.address", "address"},
		{".uverse.error", "error"},
		{"gno.land/r/gov/dao.Executor", "dao.Executor"},
		{"*gno.land/r/gov/dao.Proposal", "*dao.Proposal"},
		{"chain/runtime.Realm", "runtime.Realm"},
	} {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, cleanType(tt.in))
		})
	}
}

func TestSplitInterfaceMethods(t *testing.T) {
	t.Parallel()

	t.Run("not an interface", func(t *testing.T) {
		t.Parallel()
		require.Nil(t, splitInterfaceMethods("string"))
		require.Nil(t, splitInterfaceMethods("struct{a int}"))
	})

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, []string{}, splitInterfaceMethods("interface {}"))
	})

	t.Run("the real realm has thirteen", func(t *testing.T) {
		t.Parallel()
		require.Len(t, splitInterfaceMethods(realmV150), 13)
	})

	// A semicolon inside a nested signature is not a separator. No current gno
	// type does this, which is exactly why it would go unnoticed.
	t.Run("nested parens do not split", func(t *testing.T) {
		t.Parallel()
		got := splitInterfaceMethods("interface {F func(func(int; string)) error; G func() int}")
		require.Len(t, got, 2)
		require.Equal(t, "G func() int", got[1])
	})
}

// The bug as the user met it: INSPECT printed a 320-column line for a realm with
// two functions, because one parameter was 290 characters of interface.
func TestFormatSignatureStaysWithinWidth(t *testing.T) {
	t.Parallel()

	t.Run("the counter realm, before and after", func(t *testing.T) {
		t.Parallel()
		got := formatSignature("  ", "Increment", []string{cleanType(realmV150)}, []string{"int"})
		require.Equal(t, "  func Increment(realm) int", got)
		require.LessOrEqual(t, len(got), maxSignatureWidth)
	})

	t.Run("short signatures stay on one line", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "  func Render(string) string",
			formatSignature("  ", "Render", []string{"string"}, []string{"string"}))
	})

	t.Run("no results", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "  func Do(int)", formatSignature("  ", "Do", []string{"int"}, nil))
	})

	t.Run("several results are parenthesised", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "  func Do() (int, error)",
			formatSignature("  ", "Do", nil, []string{"int", "error"}))
	})

	// A signature that is genuinely long, because the parameters are many rather
	// than because one of them was mis-rendered, wraps instead of truncating.
	t.Run("a genuinely long signature wraps, one parameter per line", func(t *testing.T) {
		t.Parallel()
		params := []string{
			"title string", "description string", "executor dao.Executor",
			"deadline int64", "quorum int", "threshold int",
		}
		got := formatSignature("  ", "ProposeWithEverything", params, []string{"id int64", "err error"})

		for _, line := range strings.Split(got, "\n") {
			require.LessOrEqual(t, len(line), maxSignatureWidth, "line too wide: %q", line)
		}
		// Nothing lost: every parameter still appears.
		for _, p := range params {
			require.Contains(t, got, p)
		}
		require.Contains(t, got, "(id int64, err error)")
		require.Equal(t, len(params)+2, len(strings.Split(got, "\n")),
			"expected an opening line, one line per parameter, and a closing line")
	})

	// A single parameter that is itself over the limit cannot be wrapped into
	// it. It must still be printed whole rather than cut.
	t.Run("one unwrappable parameter is not truncated", func(t *testing.T) {
		t.Parallel()
		long := "x " + strings.Repeat("a", maxSignatureWidth*2)
		got := formatSignature("  ", "F", []string{long}, nil)
		require.Contains(t, got, long)
	})
}
