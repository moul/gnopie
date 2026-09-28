package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The file READ is pointed at. Written to contain, deliberately, every shape the
// old brace-counting scanner got wrong.
const sampleRealm = `package counter

import "std"

// Total is the running count.
//
// Its doc comment spans two paragraphs, because a reader asking for the source
// wants this part too.
var Total int

const (
	// Max is the ceiling.
	Max = 100
	Min = 0
)

// Config is a type.
type Config struct {
	Name string
}

// Increment bumps the counter.
func Increment(cur realm) int {
	Total++
	return Total
}

// Brace returns a literal brace, which is what broke the scanner.
func Brace() string {
	return "{"
}

// Commented mentions func Increment in its own body comment.
func Commented() {
	// func Increment is referenced here but not declared
	_ = 1
}

func (c Config) Method() string {
	return c.Name
}

// Last is last, so a runaway extraction visibly swallows it.
func Last() int { return 42 }
`

// The bug as READ met it: a brace inside a string literal never closed the
// count, so the extraction ran to the end of the file and printed every
// declaration after the one asked for.
func TestExtractDeclStopsAtTheDeclaration(t *testing.T) {
	t.Parallel()
	got := extractDecl(sampleRealm, "Brace")

	require.Contains(t, got, `return "{"`)
	require.NotContains(t, got, "func Commented", "extraction ran past the declaration")
	require.NotContains(t, got, "func Last", "extraction ran to the end of the file")
	require.Equal(t, 1, strings.Count(got, "func "), "exactly one declaration, got:\n%s", got)
}

// A comment is not a declaration. `// See func Helper for details.` used to win
// over the real thing.
func TestExtractDeclIgnoresComments(t *testing.T) {
	t.Parallel()
	src := `package x

// See func Helper for details.
const A = 1

// Helper helps.
func Helper() {}
`
	got := extractDecl(src, "Helper")
	require.Contains(t, got, "func Helper() {}")
	require.NotContains(t, got, "const A", "a comment mentioning the symbol was matched as the declaration")

	// And the reverse: a body comment naming another symbol must not make this
	// function answer for it.
	require.NotContains(t, extractDecl(sampleRealm, "Commented"), "func Increment(",
		"a comment inside the body was treated as a declaration")
}

// Doc comments come back with the declaration. go doc does this and a reader
// asking for source wants the why, not just the how.
func TestExtractDeclIncludesDocComment(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		symbol   string
		wantDoc  string
		wantCode string
	}{
		{"Increment", "// Increment bumps the counter.", "func Increment(cur realm) int {"},
		{"Config", "// Config is a type.", "type Config struct {"},
		{"Total", "// Total is the running count.", "var Total int"},
	} {
		t.Run(tt.symbol, func(t *testing.T) {
			t.Parallel()
			got := extractDecl(sampleRealm, tt.symbol)
			require.Contains(t, got, tt.wantDoc)
			require.Contains(t, got, tt.wantCode)
			require.True(t, strings.HasPrefix(got, "//"), "the doc comment should lead, got:\n%s", got)
		})
	}
}

// A grouped const returns its own entry, not the whole group, which in a real
// realm can be hundreds of unrelated lines.
func TestExtractDeclGroupedConst(t *testing.T) {
	t.Parallel()
	got := extractDecl(sampleRealm, "Max")
	require.Contains(t, got, "Max = 100")
	require.Contains(t, got, "// Max is the ceiling.")
	require.NotContains(t, got, "Min = 0", "the whole group came back instead of the one entry")
}

func TestExtractDeclMethod(t *testing.T) {
	t.Parallel()
	got := extractDecl(sampleRealm, "Method")
	require.Contains(t, got, "func (c Config) Method() string")
	require.Contains(t, got, "return c.Name")
}

func TestExtractDeclMissingSymbol(t *testing.T) {
	t.Parallel()
	require.Equal(t, "", extractDecl(sampleRealm, "NotThere"))
	// A symbol that only appears inside another function's body is not declared.
	require.Equal(t, "", extractDecl(sampleRealm, "Nope"))
}

// A plain function outranks a method of the same name.
func TestExtractDeclPrefersFunctionOverMethod(t *testing.T) {
	t.Parallel()
	src := `package x

type T struct{}

func (T) Run() string { return "method" }

func Run() string { return "function" }
`
	got := extractDecl(src, "Run")
	require.Contains(t, got, `return "function"`)
	require.NotContains(t, got, `return "method"`)
}

// When go/parser cannot read the file, the scanner still answers. gno is free to
// diverge from Go and a degraded answer beats no answer, but the fallback must
// actually be reachable.
func TestExtractDeclFallsBackOnUnparseableSource(t *testing.T) {
	t.Parallel()
	broken := `package x

this is not go at all (((

func Survivor() int {
	return 1
}
`
	_, parsed := extractDeclAST(broken, "Survivor")
	require.False(t, parsed, "this sample is supposed to defeat go/parser")

	got := extractDecl(broken, "Survivor")
	require.Contains(t, got, "func Survivor() int", "the scanner fallback did not run")
}

// A parsed file that simply lacks the symbol must NOT fall through to the
// scanner, or the false positives come back through the side door.
func TestExtractDeclDoesNotFallBackWhenParsed(t *testing.T) {
	t.Parallel()
	src := `package x

// mentions func Ghost in a comment
func Real() {}
`
	_, parsed := extractDeclAST(src, "Ghost")
	require.True(t, parsed)
	require.Equal(t, "", extractDecl(src, "Ghost"),
		"a parsed file without the symbol should answer 'not here', not defer to the scanner")
}
