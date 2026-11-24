package detect

import "testing"

func TestDiffArguments_Added(t *testing.T) {
	a := map[string]any{"name": "John"}
	b := map[string]any{"name": "John", "age": 30}

	diff := DiffArguments(a, b)
	if len(diff.Added) != 1 {
		t.Errorf("added = %d, want 1", len(diff.Added))
	}
	if diff.Added["age"] != 30 {
		t.Errorf("added[age] = %v, want 30", diff.Added["age"])
	}
}

func TestDiffArguments_Removed(t *testing.T) {
	a := map[string]any{"name": "John", "age": 30}
	b := map[string]any{"name": "John"}

	diff := DiffArguments(a, b)
	if len(diff.Removed) != 1 {
		t.Errorf("removed = %d, want 1", len(diff.Removed))
	}
	if diff.Removed["age"] != 30 {
		t.Errorf("removed[age] = %v, want 30", diff.Removed["age"])
	}
}

func TestDiffArguments_Changed(t *testing.T) {
	a := map[string]any{"sql": "SELECT name FROM users"}
	b := map[string]any{"sql": "SELECT name, ssn FROM users"}

	diff := DiffArguments(a, b)
	if len(diff.Changed) != 1 {
		t.Errorf("changed = %d, want 1", len(diff.Changed))
	}
	change, ok := diff.Changed["sql"]
	if !ok {
		t.Fatal("expected sql in changed")
	}
	if change.Before != "SELECT name FROM users" {
		t.Errorf("before = %v", change.Before)
	}
	if change.After != "SELECT name, ssn FROM users" {
		t.Errorf("after = %v", change.After)
	}
}

func TestDiffArguments_NoDiff(t *testing.T) {
	a := map[string]any{"name": "John", "age": 30}
	b := map[string]any{"name": "John", "age": 30}

	diff := DiffArguments(a, b)
	if !diff.IsEmpty() {
		t.Error("expected empty diff")
	}
}

func TestDiffArguments_AllChanges(t *testing.T) {
	a := map[string]any{"keep": "same", "change": "old", "remove": "val"}
	b := map[string]any{"keep": "same", "change": "new", "add": "val"}

	diff := DiffArguments(a, b)
	if len(diff.Added) != 1 {
		t.Errorf("added = %d", len(diff.Added))
	}
	if len(diff.Removed) != 1 {
		t.Errorf("removed = %d", len(diff.Removed))
	}
	if len(diff.Changed) != 1 {
		t.Errorf("changed = %d", len(diff.Changed))
	}
}
