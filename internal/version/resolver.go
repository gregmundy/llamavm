package version

// Resolver answers "which tag should a shim invocation use?".
// In M2 it is a thin wrapper around Store.Active. M3 will layer in
// the cwd-walk for .llama-version files in front of the current-file
// fallback by changing this method body — call sites stay identical.
type Resolver struct {
	store *Store
}

// NewResolver wraps a Store. The store must be non-nil.
func NewResolver(s *Store) *Resolver {
	return &Resolver{store: s}
}

// Resolve returns the active tag or ErrNoActiveVersion.
func (r *Resolver) Resolve() (string, error) {
	return r.store.Active()
}
