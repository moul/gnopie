package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// Pulling one declaration out of a .gno file, for `READ <pkg>.<Symbol>`.
//
// This was a line scanner that looked for "func "+symbol and then counted braces
// to find the end. It was wrong in two ways that both produce silent garbage
// rather than an error, which is the worst kind:
//
//  1. A brace inside a string literal broke the count. `func WithBrace() string
//     { return "{" }` never returned to depth zero, so READ printed that function
//     AND the entire rest of the file.
//
//  2. A comment was indistinguishable from code. `// See func Helper for
//     details.` matched before the real declaration and READ returned the
//     comment, whatever followed it, and then the function.
//
// gno is syntactically Go for declarations, so go/parser handles it: measured
// 2026-09-28 against every .gno file under a v1.5.0 examples/ tree, 1036 parsed
// and 0 failed. Parsing gives exact boundaries, real name matching, and the doc
// comment for free, which a reader wanted anyway.
//
// The line scanner is kept as a fallback for a file go/parser cannot handle,
// since gno is free to diverge from Go and a degraded answer beats no answer.

// extractDecl returns the source of a single declaration (func, method, type,
// var or const), including its doc comment, or "" if the symbol is not declared
// in this file.
func extractDecl(source, symbol string) string {
	if decl, ok := extractDeclAST(source, symbol); ok {
		return decl
	}
	return extractDeclByLines(source, symbol)
}

// extractDeclAST finds the declaration by parsing. The bool reports whether the
// file parsed at all, NOT whether the symbol was found: a parsed file that does
// not declare the symbol is a definitive "not here", and must not fall through
// to the scanner and its false positives.
func extractDeclAST(source, symbol string) (string, bool) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "src.gno", source, parser.ParseComments)
	if err != nil {
		return "", false
	}

	offset := func(p token.Pos) int { return fset.Position(p).Offset }
	// span returns the source between two positions, extended backwards over a
	// doc comment when there is one.
	span := func(doc *ast.CommentGroup, start, end token.Pos) string {
		from := offset(start)
		if doc != nil {
			from = offset(doc.Pos())
		}
		to := offset(end)
		if to > len(source) {
			to = len(source)
		}
		if from < 0 || from > to {
			return ""
		}
		return strings.TrimRight(source[from:to], "\n")
	}

	// A plain function wins over a method of the same name: `READ pkg.Foo` means
	// the package-level Foo if there is one.
	var method string
	for _, d := range file.Decls {
		switch decl := d.(type) {
		case *ast.FuncDecl:
			if decl.Name.Name != symbol {
				continue
			}
			got := span(decl.Doc, decl.Pos(), decl.End())
			if decl.Recv == nil {
				return got, true
			}
			if method == "" {
				method = got
			}

		case *ast.GenDecl:
			for _, spec := range decl.Specs {
				if !specDeclares(spec, symbol) {
					continue
				}
				// An ungrouped declaration is returned whole, parentheses and
				// all. A grouped one returns only its own spec, because the
				// group can be hundreds of lines of unrelated constants.
				if !decl.Lparen.IsValid() {
					return span(decl.Doc, decl.Pos(), decl.End()), true
				}
				return span(specDoc(spec), spec.Pos(), spec.End()), true
			}
		}
	}
	return method, true
}

// specDeclares reports whether a spec declares the named symbol.
func specDeclares(spec ast.Spec, symbol string) bool {
	switch s := spec.(type) {
	case *ast.TypeSpec:
		return s.Name.Name == symbol
	case *ast.ValueSpec:
		for _, n := range s.Names {
			if n.Name == symbol {
				return true
			}
		}
	}
	return false
}

// specDoc returns a spec's own doc comment, for the grouped case.
func specDoc(spec ast.Spec) *ast.CommentGroup {
	switch s := spec.(type) {
	case *ast.TypeSpec:
		return s.Doc
	case *ast.ValueSpec:
		return s.Doc
	}
	return nil
}

// extractDeclByLines is the original scanner, kept only for a file go/parser
// cannot read. It counts braces and cannot tell a string literal or a comment
// from code; see the package comment above for what that costs. Do not extend
// it: fix extractDeclAST instead.
func extractDeclByLines(source, symbol string) string {
	lines := strings.Split(source, "\n")
	prefixes := []string{"func ", "var ", "type ", "const "}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// A comment is not a declaration. The scanner had no such check, which
		// is how `// See func Helper for details.` used to win.
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		for _, prefix := range prefixes {
			if !strings.Contains(trimmed, prefix+symbol) {
				continue
			}
			idx := strings.Index(trimmed, prefix+symbol)
			afterSymbol := idx + len(prefix) + len(symbol)
			if afterSymbol < len(trimmed) {
				next := trimmed[afterSymbol]
				if next != '(' && next != ' ' && next != '\t' && next != '{' && next != '\n' {
					continue // substring match, skip
				}
			}

			if prefix == "var " || prefix == "const " {
				if !strings.Contains(line, "{") {
					return strings.TrimRight(line, "\n")
				}
			}

			var result strings.Builder
			depth := 0
			started := false
			for j := i; j < len(lines); j++ {
				result.WriteString(lines[j])
				result.WriteByte('\n')
				for _, ch := range lines[j] {
					switch ch {
					case '{':
						depth++
						started = true
					case '}':
						depth--
					}
				}
				if started && depth == 0 {
					return strings.TrimRight(result.String(), "\n")
				}
			}
			return strings.TrimRight(result.String(), "\n")
		}
	}
	return ""
}
