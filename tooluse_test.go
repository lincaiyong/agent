package agent

import (
	"fmt"
	"testing"
)

func TestFoo(t *testing.T) {
	text := `<tool_use name="rg">DistributorCode.*auth|auth.*DistributorCode</tool_use>`
	uses := ExtractToolUses(text)
	if len(uses) != 1 {
		t.Fatalf("len(uses) = %d; want 1", len(uses))
	}
	err := uses[0].Call("/tmp/sample")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(uses[0].Result)
}
