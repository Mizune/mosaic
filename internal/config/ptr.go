package config

// BoolPtr returns a pointer to v. It reads cleanly in struct
// literals where a nil / &true / &false distinction is load-
// bearing — e.g. the [docker] section where nil means "inherit"
// and &false means "explicit opt-out".
func BoolPtr(v bool) *bool { return &v }

// BoolOr returns *p when p is non-nil, otherwise fallback.
// Callers should prefer this helper over ad-hoc nil checks so
// the "default when unset" value lives at the read site instead
// of being scattered across guard clauses.
func BoolOr(p *bool, fallback bool) bool {
	if p == nil {
		return fallback
	}
	return *p
}
