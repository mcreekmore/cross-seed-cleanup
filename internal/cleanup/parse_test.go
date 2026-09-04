package cleanup

import "testing"

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
