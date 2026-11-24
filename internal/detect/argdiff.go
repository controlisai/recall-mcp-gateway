package detect

import "reflect"

// DiffArguments compares two argument maps and returns added, removed, and changed fields.
func DiffArguments(argsA, argsB map[string]any) *ArgumentDiff {
	diff := &ArgumentDiff{
		Added:   make(map[string]any),
		Removed: make(map[string]any),
		Changed: make(map[string]ArgumentChange),
	}

	for k, v := range argsB {
		if prev, exists := argsA[k]; !exists {
			diff.Added[k] = v
		} else if !reflect.DeepEqual(prev, v) {
			diff.Changed[k] = ArgumentChange{Before: prev, After: v}
		}
	}
	for k, v := range argsA {
		if _, exists := argsB[k]; !exists {
			diff.Removed[k] = v
		}
	}

	return diff
}

// IsEmpty returns true if there are no differences.
func (d *ArgumentDiff) IsEmpty() bool {
	return len(d.Added) == 0 && len(d.Removed) == 0 && len(d.Changed) == 0
}
