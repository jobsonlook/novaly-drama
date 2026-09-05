package controllers

import "testing"

func TestNeedsExactDoubaoTrim(t *testing.T) {
	for _, seconds := range []int{1, 4, 6, 7, 9, 11, 14} {
		if !needsExactDoubaoTrim(seconds) {
			t.Fatalf("%ds should be trimmed", seconds)
		}
	}
	for _, seconds := range []int{0, 5, 10, 15, 16, 30} {
		if needsExactDoubaoTrim(seconds) {
			t.Fatalf("%ds should not be trimmed", seconds)
		}
	}
}
