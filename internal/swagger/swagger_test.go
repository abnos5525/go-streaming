package swagger

import "testing"

func TestSpec(t *testing.T) {
	if len(spec()) < 100 {
		t.Fatal("empty spec")
	}
}
