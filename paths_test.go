package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// ParsePath is the whole front door: every verb starts by handing it whatever the
// user typed, and until now it was only ever exercised through a running chain.
// It is pure, so it does not need one.
func TestParsePath(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		in   string

		wantKind    PathKind
		wantDomain  string
		wantPkgPath string
		wantSymbol  string
		wantFile    string
		wantAddress string
		wantRender  string
		wantArgs    []string
		wantErr     string
	}{
		// --- networks ---
		{
			name: "bare domain", in: "gno.land",
			wantKind: PathNetwork, wantDomain: "gno.land",
		},
		{
			name: "domain with trailing slash", in: "gno.land/",
			wantKind: PathNetwork, wantDomain: "gno.land",
		},

		// --- namespaces and packages ---
		{
			name: "namespace", in: "gno.land/r/gnoland",
			wantKind: PathNamespace, wantDomain: "gno.land", wantPkgPath: "gno.land/r/gnoland",
		},
		{
			name: "package", in: "gno.land/r/gnoland/blog",
			wantKind: PathPackage, wantDomain: "gno.land", wantPkgPath: "gno.land/r/gnoland/blog",
		},
		{
			name: "deep package", in: "gno.land/p/nt/tinyavl/v0",
			wantKind: PathPackage, wantDomain: "gno.land", wantPkgPath: "gno.land/p/nt/tinyavl/v0",
		},

		// --- symbols ---
		{
			name: "symbol", in: "gno.land/r/gnoland/blog.ModAddPost",
			wantKind: PathSymbol, wantDomain: "gno.land",
			wantPkgPath: "gno.land/r/gnoland/blog", wantSymbol: "ModAddPost",
		},

		// --- calls ---
		{
			name: "call with no args", in: "gno.land/r/demo/counter.Increment()",
			wantKind: PathCall, wantDomain: "gno.land",
			wantPkgPath: "gno.land/r/demo/counter", wantSymbol: "Increment",
		},
		{
			name: "call with a string arg", in: `gno.land/r/gnoland/blog.Render("")`,
			wantKind: PathCall, wantPkgPath: "gno.land/r/gnoland/blog",
			wantSymbol: "Render", wantArgs: []string{""},
		},
		{
			name: "call with a numeric arg", in: "gno.land/r/gnoland/wugnot.Withdraw(102639)",
			wantKind: PathCall, wantPkgPath: "gno.land/r/gnoland/wugnot",
			wantSymbol: "Withdraw", wantArgs: []string{"102639"},
		},
		{
			name: "call with several args", in: `gno.land/r/demo/x.F("a", 2, true)`,
			wantKind: PathCall, wantPkgPath: "gno.land/r/demo/x",
			wantSymbol: "F", wantArgs: []string{"a", "2", "true"},
		},
		{
			name: "a comma inside a quoted arg is not a separator", in: `gno.land/r/demo/x.F("a,b", "c")`,
			wantKind: PathCall, wantPkgPath: "gno.land/r/demo/x",
			wantSymbol: "F", wantArgs: []string{"a,b", "c"},
		},
		{
			name: "single quotes work too", in: `gno.land/r/demo/x.F('hi')`,
			wantKind: PathCall, wantPkgPath: "gno.land/r/demo/x",
			wantSymbol: "F", wantArgs: []string{"hi"},
		},

		// --- files ---
		{
			name: "gno file", in: "gno.land/r/gnoland/blog/admin.gno",
			wantKind: PathFile, wantDomain: "gno.land",
			wantPkgPath: "gno.land/r/gnoland/blog", wantFile: "admin.gno",
		},
		{
			name: "toml file", in: "gno.land/r/gnoland/blog/gnomod.toml",
			wantKind: PathFile, wantPkgPath: "gno.land/r/gnoland/blog", wantFile: "gnomod.toml",
		},

		// --- addresses and users ---
		{
			name: "bech32 address", in: "g1manfred47kzduec920z88wfr64ylksmdcedlf5",
			wantKind: PathAddress, wantAddress: "g1manfred47kzduec920z88wfr64ylksmdcedlf5",
		},
		{
			name: "gnoweb user url", in: "gno.land/u/moul",
			wantKind: PathUser, wantDomain: "gno.land", wantSymbol: "moul",
		},

		// --- gnoweb URLs, the paste-it-straight-in promise ---
		{
			name: "https prefix is stripped", in: "https://gno.land/r/gnoland/blog",
			wantKind: PathPackage, wantPkgPath: "gno.land/r/gnoland/blog",
		},
		{
			name: "http prefix is stripped", in: "http://gno.land/r/gnoland/blog",
			wantKind: PathPackage, wantPkgPath: "gno.land/r/gnoland/blog",
		},
		{
			name: "render path after a colon", in: "https://gno.land/r/gnoland/blog:p/beta-mainnet",
			wantKind: PathPackage, wantPkgPath: "gno.land/r/gnoland/blog", wantRender: "p/beta-mainnet",
		},
		{
			name: "dollar-source modifier", in: "gno.land/r/gnoland/blog$source",
			wantKind: PathPackage, wantPkgPath: "gno.land/r/gnoland/blog", wantRender: "$source",
		},
		{
			name: "dollar-source with a file param", in: "gno.land/r/gnoland/blog$source&file=admin.gno",
			wantKind: PathPackage, wantPkgPath: "gno.land/r/gnoland/blog",
			wantRender: "$source", wantFile: "admin.gno",
		},
		{
			name: "dollar-help modifier", in: "gno.land/r/gnoland/blog$help",
			wantKind: PathPackage, wantPkgPath: "gno.land/r/gnoland/blog", wantRender: "$help",
		},
		{
			name: "a func fragment names the symbol", in: "gno.land/r/gnoland/blog$help#func-ModAddPost",
			wantKind: PathPackage, wantPkgPath: "gno.land/r/gnoland/blog",
			wantRender: "$help", wantSymbol: "ModAddPost",
		},
		{
			name: "an unrelated fragment is dropped", in: "https://gno.land/r/demo/counter#some-anchor",
			wantKind: PathPackage, wantPkgPath: "gno.land/r/demo/counter",
		},

		// --- errors ---
		{name: "empty", in: "", wantErr: "empty path"},
		{name: "only whitespace", in: "   ", wantErr: "empty path"},
		{
			name: "call with no function name", in: "gno.land/r/demo/counter(1)",
			wantErr: "call expression requires a function name",
		},
		{
			name: "unterminated string", in: `gno.land/r/demo/x.F("oops)`,
			wantErr: "unterminated string",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := ParsePath(tt.in)
			if tt.wantErr != "" {
				require.Error(t, err, "expected an error for %q", tt.in)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantKind, p.Kind, "kind")
			if tt.wantDomain != "" {
				require.Equal(t, tt.wantDomain, p.Domain, "domain")
			}
			require.Equal(t, tt.wantPkgPath, p.PkgPath, "pkgpath")
			require.Equal(t, tt.wantSymbol, p.Symbol, "symbol")
			require.Equal(t, tt.wantFile, p.File, "file")
			require.Equal(t, tt.wantAddress, p.Address, "address")
			require.Equal(t, tt.wantRender, p.RenderPath, "renderpath")
			require.Equal(t, tt.wantArgs, p.Args, "args")
		})
	}
}

// The distinction that decides whether gnopie will even try to call something.
func TestGnoPathIsPublic(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		in   string
		want bool
	}{
		{"gno.land/r/demo/x.Increment()", true},
		{"gno.land/r/demo/x.increment()", false},
		{"gno.land/r/demo/x._hidden()", false},
		{"gno.land/r/demo/x", false}, // no symbol at all
	} {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			p, err := ParsePath(tt.in)
			require.NoError(t, err)
			require.Equal(t, tt.want, p.IsPublic())
		})
	}
}

func TestParseCallArgs(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		in      string
		want    []string
		wantErr string
	}{
		{name: "empty call", in: "()", want: nil},
		{name: "whitespace only", in: "(  )", want: nil},
		{
			// The empty STRING is an argument; the empty argument list is not.
			// Telling those apart is the reason parseCallArgs tracks quoting at
			// all, and Render("") is the single most common call anybody makes.
			name: "the empty string is one argument", in: `("")`, want: []string{""},
		},
		{name: "one bare word", in: "(hello)", want: []string{"hello"}},
		{name: "quotes are stripped", in: `("hello")`, want: []string{"hello"}},
		{name: "spaces around args are trimmed", in: `( "a" , "b" )`, want: []string{"a", "b"}},
		{name: "an escaped quote survives", in: `("a\"b")`, want: []string{`a\"b`}},
		{name: "numbers stay bare", in: "(1, -2, 3)", want: []string{"1", "-2", "3"}},
		{name: "mixed quoting", in: `(1, "two", true)`, want: []string{"1", "two", "true"}},

		{name: "no parens", in: "nope", wantErr: "invalid call syntax"},
		{name: "unclosed", in: "(a", wantErr: "invalid call syntax"},
		{name: "unterminated quote", in: `("a)`, wantErr: "unterminated string"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseCallArgs(tt.in)
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

// joinArgs decides what reaches the chain as a quoted string and what reaches it
// bare, which is the difference between calling F(1) and F("1").
func TestJoinArgs(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		in   []string
		want string
	}{
		{"nothing", nil, ""},
		{"a number stays bare", []string{"42"}, "42"},
		{"a negative number stays bare", []string{"-42"}, "-42"},
		{"true stays bare", []string{"true"}, "true"},
		{"false stays bare", []string{"false"}, "false"},
		{"a word is quoted", []string{"hello"}, `"hello"`},
		{"the empty string is quoted", []string{""}, `""`},
		{"a number-like word is quoted", []string{"4x"}, `"4x"`},
		{"mixed", []string{"1", "two", "true"}, `1,"two",true`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, joinArgs(tt.in))
		})
	}
}

// joinRunArgs is joinArgs plus the one token that must NOT be quoted, because it
// is Gno syntax rather than a value. Quoting it is how the generated script
// stopped type-checking the first time.
func TestJoinRunArgs(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		in   []string
		want string
	}{
		{"the cross sentinel becomes syntax", []string{"__cross__"}, "cross(cur)"},
		{"cross then a value", []string{"__cross__", "hi"}, `cross(cur), "hi"`},
		{"cross then a number", []string{"__cross__", "7"}, "cross(cur), 7"},
		{"no cross at all", []string{"hi"}, `"hi"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, joinRunArgs(tt.in))
		})
	}
}

func TestIsNumeric(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		in   string
		want bool
	}{
		{"0", true}, {"42", true}, {"-42", true},
		{"", false}, {"-", false}, {"4x", false}, {"x4", false},
		{"4.2", false}, // gno has no float literal in a call argument
		{"4-2", false}, // a minus is only a sign, and only in front
	} {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, isNumeric(tt.in))
		})
	}
}

func TestIsFileExtension(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		in   string
		want bool
	}{
		{"admin.gno", true}, {"gnomod.toml", true}, {"README.md", true},
		{"notes.txt", true}, {"data.json", true},
		{"Increment", false}, {"blog", false}, {"file.go", false},
	} {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, isFileExtension(tt.in))
		})
	}
}

// generateRunCode is the function that broke when the VM moved, and it broke
// silently: the symptom was `invalid gno package; type check failed`, which names
// neither the version nor the line. These assertions are the spelling, written
// down, so the next move is caught here rather than against a chain.
func TestGenerateRunCode(t *testing.T) {
	t.Parallel()

	t.Run("a crossing call", func(t *testing.T) {
		t.Parallel()
		got := generateRunCode("gno.land/r/demo/counter", "counter", "Increment", []string{"__cross__"})
		require.Equal(t, `package main

import "gno.land/r/demo/counter"

func main(cur realm) {
	counter.Increment(cross(cur))
}
`, got)
	})

	t.Run("a non-crossing call with arguments", func(t *testing.T) {
		t.Parallel()
		got := generateRunCode("gno.land/r/demo/x", "x", "F", []string{"hi", "7"})
		require.Equal(t, `package main

import "gno.land/r/demo/x"

func main(cur realm) {
	x.F("hi", 7)
}
`, got)
	})

	// The two rules preprocess.go states, asserted rather than described: the
	// entry point takes a realm, and cross is handed the identifier main binds.
	t.Run("the entry point signature is the current one", func(t *testing.T) {
		t.Parallel()
		got := generateRunCode("gno.land/r/demo/counter", "counter", "Increment", []string{"__cross__"})
		require.Contains(t, got, "func main(cur realm)", "main must take a realm")
		require.NotContains(t, got, "func main()", "the bare entry point is the pre-v1.5 spelling")
		require.Contains(t, got, "cross(cur)", "cross must name the realm main binds")
		require.NotContains(t, got, "(cross,", "a bare cross is the pre-v1.5 spelling")
	})
}
