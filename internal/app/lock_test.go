package app

import "testing"

// TestBaseLock verifies that a second acquisition of the base-directory lock fails.
func TestBaseLock(t *testing.T) {
	base := t.TempDir()

	unlock, err := lockBase(base)
	if err != nil {
		t.Fatal(err)
	}

	defer unlock()

	if _, err = lockBase(base); err == nil {
		t.Fatal("concurrent lock accepted")
	}
}
