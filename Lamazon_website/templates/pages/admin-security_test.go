package pages

import (
	"strings"
	"testing"
)

func TestLegacyCSVFormulaInjectionIsNeutralized(t *testing.T) {
	for _, value := range []string{"=HYPERLINK(\"https://evil.invalid\",\"open\")", "+CMD", "-1+1", "@SUM(A1:A2)", " \t=1+1", "\uFEFF=1+1", "\tvalue"} {
		if got := safeCSVCell(value); !strings.HasPrefix(got, "'") {
			t.Errorf("unsafe CSV %q", value)
		}
	}
	if safeCSVCell("PURE BITES") != "PURE BITES" {
		t.Fatal("ordinary text changed")
	}
}
