package main

import (
	"testing"
)

func TestGetenv(t *testing.T) {
	t.Setenv("_CSC_TEST_KEY", "hello")
	if got := getenv("_CSC_TEST_KEY", "default"); got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
	if got := getenv("_CSC_MISSING_KEY", "default"); got != "default" {
		t.Errorf("got %q, want %q", got, "default")
	}
}

func TestGetenvInt(t *testing.T) {
	t.Setenv("_CSC_INT_KEY", "42")
	if got := getenvInt("_CSC_INT_KEY", 0); got != 42 {
		t.Errorf("got %d, want 42", got)
	}
	if got := getenvInt("_CSC_MISSING_INT", 99); got != 99 {
		t.Errorf("got %d, want 99", got)
	}
	t.Setenv("_CSC_INT_KEY", "not-a-number")
	if got := getenvInt("_CSC_INT_KEY", 5); got != 5 {
		t.Errorf("invalid int should fall back, got %d", got)
	}
}

func TestGetenvBool(t *testing.T) {
	cases := []struct {
		val      string
		fallback bool
		want     bool
	}{
		{"true", false, true},
		{"1", false, true},
		{"yes", false, true},
		{"TRUE", false, true},
		{"false", true, false},
		{"0", true, false},
		{"no", true, false},
		{"garbage", true, true}, // unrecognized -> fallback
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.val, func(t *testing.T) {
			t.Setenv("_CSC_BOOL_KEY", tc.val)
			if got := getenvBool("_CSC_BOOL_KEY", tc.fallback); got != tc.want {
				t.Errorf("getenvBool(%q, %v) = %v, want %v", tc.val, tc.fallback, got, tc.want)
			}
		})
	}
	// unset key -> fallback
	if got := getenvBool("_CSC_DEFINITELY_MISSING_BOOL", true); !got {
		t.Error("unset key should return fallback true")
	}
}

func TestSplitSet(t *testing.T) {
	got := splitSet("pinned, keep,  tagged")
	for _, want := range []string{"pinned", "keep", "tagged"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing %q in result", want)
		}
	}
	if _, ok := got[""]; ok {
		t.Error("empty string should not be in set")
	}
	if len(splitSet("")) != 0 {
		t.Error("splitSet(\"\") should return empty set")
	}
	// single value, no comma
	if _, ok := splitSet("only")["only"]; !ok {
		t.Error("splitSet(\"only\") should contain \"only\"")
	}
}
