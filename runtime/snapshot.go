package runtime

import "sort"

// Snapshot identifies a committed state revision that can be revisited.
type Snapshot struct {
	Revision int
	Label    string
}

// labels stores user-provided labels keyed by committed revision number.
// Labels are intentionally held in process memory for this local runtime.
// The state itself is fully durable across restarts via the backing store.

// ListSnapshots returns the labeled snapshots recorded against the store.
func (r *Runtime) ListSnapshots() ([]Snapshot, error) {
	r.labelMu.Lock()
	defer r.labelMu.Unlock()
	revisions := make([]int, 0, len(r.labels))
	for rev := range r.labels {
		revisions = append(revisions, rev)
	}
	sort.Ints(revisions)
	out := make([]Snapshot, 0, len(revisions))
	for _, rev := range revisions {
		out = append(out, Snapshot{Revision: rev, Label: r.labels[rev]})
	}
	return out, nil
}

// LabelSnapshot marks the current committed revision with a label.
func (r *Runtime) LabelSnapshot(label string) (Snapshot, error) {
	r.labelMu.Lock()
	defer r.labelMu.Unlock()
	rev := r.store.Revision()
	r.labels[rev] = label
	return Snapshot{Revision: rev, Label: label}, nil
}

// Revision returns the current committed state revision.
func (r *Runtime) Revision() int { return r.store.Revision() }
