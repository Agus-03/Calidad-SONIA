package id

import "testing"

// TestNewReturnsVersion7 protects C04: version 4 is random, scatters the inserts and fragments the index.
func TestNewReturnsVersion7(t *testing.T) {
	for i := 0; i < 100; i++ {
		got := New().Version()
		if got != 7 {
			t.Fatalf("New() returned version %d, want 7", got)
		}
	}
}
