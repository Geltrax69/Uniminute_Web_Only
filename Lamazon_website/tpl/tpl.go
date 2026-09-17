package tpl

// Tiny template helpers shared by every templ package.

// When is the ternary templates need for class strings and attributes.
func When(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

// Attr returns the value or the empty string, for optional attributes an
// empty value would otherwise still render.
func Attr(cond bool, v string) string {
	if cond {
		return v
	}
	return ""
}
