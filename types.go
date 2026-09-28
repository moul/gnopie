package main

import (
	"fmt"
	"strings"
)

// Rendering the types that `vm/qfuncs` reports.
//
// qfuncs returns a type's STRUCTURE, not the name it was declared under, so a
// crossing function's first parameter comes back as the whole realm interface
// spelled out:
//
//	interface {.seal func(); Address func() address; IsCode func() bool; ...}
//
// which is 290 characters for one parameter, and made `INSPECT` print a
// 320-column line for a realm with two functions in it.
//
// There was already a fix for this, and it is the cautionary tale: a constant
// holding the literal prefix
//
//	"interface {Address func() .uverse.address; Coins func() .uverse.gnocoins;"
//
// which was exactly right when it was written and matched nothing by v1.5.0,
// because the VM had added `.seal`, dropped `Coins` and stopped qualifying
// `address` with `.uverse`. It failed silently: no error, just the raw interface
// back in the output, which is how it survived long enough to reach a README
// screenshot.
//
// So this matches on the SHAPE, not on a string: parse the method set and decide
// from it, with a quorum rather than an exact list, because the VM adding a
// thirteenth method is the normal course of events and must not break this again.

const (
	// maxSignatureWidth is where a function signature gets broken across lines.
	// A fixed number rather than the terminal width on purpose: the README's
	// screenshots are generated, and a capture whose line breaks depend on the
	// window that produced it would differ on every machine and never match in
	// CI.
	maxSignatureWidth = 88

	// continuationIndent is the leading space on a wrapped parameter line.
	continuationIndent = "      "
)

// realmMethods is the quorum that identifies gno's realm interface.
//
// A subset, deliberately. Requiring the full method set is what broke the last
// attempt: the VM changes it. These five have been there across every version
// this tool has seen, and no other interface in the standard library carries
// them together.
var realmMethods = []string{"Address", "PkgPath", "IsUser", "Previous", "String"}

// splitInterfaceMethods returns the method declarations inside an anonymous
// interface type, or nil if s is not one.
//
// Splitting is on "; " at nesting depth zero: a method like
// `Sub func(string) realm` carries parentheses, and a nested interface or struct
// would carry its own semicolons.
func splitInterfaceMethods(s string) []string {
	body, ok := strings.CutPrefix(strings.TrimSpace(s), "interface {")
	if !ok {
		return nil
	}
	body, ok = strings.CutSuffix(strings.TrimSpace(body), "}")
	if !ok {
		return nil
	}
	if strings.TrimSpace(body) == "" {
		return []string{}
	}

	var (
		methods []string
		cur     strings.Builder
		depth   int
	)
	for i := 0; i < len(body); i++ {
		switch c := body[i]; c {
		case '(', '{', '[':
			depth++
			cur.WriteByte(c)
		case ')', '}', ']':
			depth--
			cur.WriteByte(c)
		case ';':
			if depth == 0 {
				methods = append(methods, strings.TrimSpace(cur.String()))
				cur.Reset()
				continue
			}
			cur.WriteByte(c)
		default:
			cur.WriteByte(c)
		}
	}
	if last := strings.TrimSpace(cur.String()); last != "" {
		methods = append(methods, last)
	}
	return methods
}

// methodName returns the identifier a method declaration starts with.
func methodName(decl string) string {
	name, _, _ := strings.Cut(strings.TrimSpace(decl), " ")
	return name
}

// methodNameSet is the set of names an interface declares, unexported entries
// (`.seal`) included, since their presence is itself informative.
func methodNameSet(methods []string) map[string]bool {
	set := make(map[string]bool, len(methods))
	for _, m := range methods {
		if n := methodName(m); n != "" {
			set[n] = true
		}
	}
	return set
}

// isRealmInterface reports whether an interface's method set is gno's realm.
func isRealmInterface(methods []string) bool {
	if len(methods) < len(realmMethods) {
		return false
	}
	set := methodNameSet(methods)
	for _, want := range realmMethods {
		if !set[want] {
			return false
		}
	}
	return true
}

// isErrorInterface reports whether an interface is the error interface.
func isErrorInterface(methods []string) bool {
	return len(methods) == 1 && methodName(methods[0]) == "Error"
}

// cleanType turns the VM's structural type spelling into something readable.
func cleanType(t string) string {
	t = strings.TrimSpace(t)

	// .uverse qualifiers are an implementation detail of the VM's universe
	// block and mean nothing to a reader.
	t = strings.ReplaceAll(t, ".uverse.", "")

	if methods := splitInterfaceMethods(t); methods != nil {
		switch {
		case isRealmInterface(methods):
			return "realm"
		case isErrorInterface(methods):
			return "error"
		case len(methods) == 0:
			return "any"
		default:
			// Unrecognised, and possibly enormous. Name it by its method set
			// rather than reproducing it: the names are the useful part, and a
			// reader who wants the full thing has READ and $source.
			names := make([]string, 0, len(methods))
			for _, m := range methods {
				if n := methodName(m); n != "" && !strings.HasPrefix(n, ".") {
					names = append(names, n)
				}
			}
			if len(names) == 0 {
				return "interface{...}"
			}
			return "interface{" + strings.Join(names, "; ") + "}"
		}
	}

	t = shortenQualifiedTypes(t)
	return t
}

// cleanParamName cleans up internal parameter names.
// Removes synthetic names like ".arg_0", ".res.0", etc.
func cleanParamName(name string) string {
	if name == "" {
		return ""
	}
	if strings.HasPrefix(name, ".arg_") || strings.HasPrefix(name, ".res.") {
		return ""
	}
	return name
}

// shortenQualifiedTypes replaces fully qualified type paths with short names.
// e.g. "gno.land/r/gov/dao.Executor" -> "dao.Executor"
// e.g. "*gno.land/r/gov/dao.Proposal" -> "*dao.Proposal"
func shortenQualifiedTypes(t string) string {
	for {
		idx := strings.Index(t, "gno.land/")
		if idx < 0 {
			break
		}
		prefix := t[:idx]
		rest := t[idx:]
		end := len(rest)
		for i, ch := range rest {
			if ch == ' ' || ch == ',' || ch == '}' || ch == ')' || ch == ';' {
				end = i
				break
			}
		}
		qualifiedName := rest[:end]
		remainder := rest[end:]

		shortName := qualifiedName
		if lastSlash := strings.LastIndex(qualifiedName, "/"); lastSlash >= 0 {
			shortName = qualifiedName[lastSlash+1:]
		}
		t = prefix + shortName + remainder
	}
	return strings.ReplaceAll(t, "chain/runtime.", "runtime.")
}

// formatSignature renders one function signature, wrapping it across lines when
// it would otherwise run past maxSignatureWidth.
//
// Wrapping rather than truncating, because a signature is the thing INSPECT
// exists to show and a cut one is worse than a long one. One parameter per
// continuation line, which is also how the same signature would be written in
// source.
func formatSignature(indent, name string, params, results []string) string {
	head := fmt.Sprintf("%sfunc %s(%s)", indent, name, strings.Join(params, ", "))
	tail := resultSuffix(results)

	if len(head)+len(tail) <= maxSignatureWidth || len(params) == 0 {
		return head + tail
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%sfunc %s(\n", indent, name)
	for i, p := range params {
		sep := ","
		if i == len(params)-1 {
			sep = ""
		}
		fmt.Fprintf(&sb, "%s%s%s%s\n", indent, continuationIndent, p, sep)
	}
	fmt.Fprintf(&sb, "%s)%s", indent, tail)
	return sb.String()
}

// resultSuffix renders a function's results, parenthesised only when there is
// more than one, as Go itself writes them.
func resultSuffix(results []string) string {
	switch len(results) {
	case 0:
		return ""
	case 1:
		return " " + results[0]
	default:
		return " (" + strings.Join(results, ", ") + ")"
	}
}
