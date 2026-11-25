package models

// Pointer helpers for config fields whose zero value is a legitimate
// setting, not "unset".
//
// Huma applies a field's `default:` tag to ANY zero value it decodes from a
// JSON request body (see huma v2 parseBodyInto: `if item.IsZero()`), so an
// explicit 0 or false in a config PUT is silently replaced by the default.
// For plain int/bool fields there is no way to tell "the client sent 0"
// from "the client omitted the field". Pointer fields fix that: huma only
// fills the default when the pointer is nil (field absent), and an explicit
// 0/false arrives as a non-nil pointer that keeps its value.
//
// The same trap applies on the YAML side in reverse: `omitempty` drops a 0,
// and a reload then re-applies the default. A non-nil pointer is never
// empty, so `index: 0` is written to the file explicitly and survives a
// reload. Config.Validate normalizes any nil left by an old file (or a YAML
// PUT that omits the key) to the documented default, so served configs and
// saved files always show an explicit value.

// IntPtr returns a pointer to i, for config literals in code and tests.
func IntPtr(i int) *int { return &i }

// BoolPtr returns a pointer to b, for config literals in code and tests.
func BoolPtr(b bool) *bool { return &b }

// DerefInt returns *p, or def when p is nil (field absent). Use at every
// read site so a nil left by an old config file still behaves as the
// documented default.
func DerefInt(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

// DerefBool returns *p, or def when p is nil (field absent).
func DerefBool(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}
