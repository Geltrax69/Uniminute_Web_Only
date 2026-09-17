package pages

import "testing"

// A "## " line is a heading on its own, and the paragraph under it joins its
// hard-wrapped lines while keeping list lines on their own.
func TestPolicyBlocks(t *testing.T) {
	got := policyBlocks("## Who we are\nLamazon is a\ncampus shop.\n\nItems:\n- one\n- two")
	want := []policyBlock{
		{Heading: true, Text: "Who we are"},
		{Text: "Lamazon is a campus shop."},
		{Text: "Items:\n- one\n- two"},
	}
	if len(got) != len(want) {
		t.Fatalf("policyBlocks = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("block %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
