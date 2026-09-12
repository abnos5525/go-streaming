package algo

import (
	"testing"

	"project1/internal/stream/ai"
)

func frames(ts ...int64) []*ai.AIFrame {
	out := make([]*ai.AIFrame, len(ts))
	for i, t := range ts {
		f := &ai.AIFrame{}
		f.Frame.Timestamp = t
		out[i] = f
	}
	return out
}

func TestHoldTimestamp(t *testing.T) {
	base := int64(1_788_870_540_661)
	var ts []int64
	for i := 0; i <= 10; i++ {
		ts = append(ts, base+int64(i)*1000)
	}
	fs := frames(ts...)
	arr := make([]int64, len(ts))
	for i := range arr {
		arr[i] = 990000 + int64(i)*1000
	}
	got := Hold(fs, arr, 8000, 1000000, 0)
	if got == nil || got.Frame.Timestamp != base+2000 {
		t.Fatalf("want held-8s, got %v", got)
	}
	if got := Hold(fs, arr, 0, 1000000, 0); got == nil || got.Frame.Timestamp != base+10000 {
		t.Fatalf("lag 0 must be latest, got %v", got)
	}
}

func TestHoldJitter(t *testing.T) {
	fs := frames(1000, 2000, 3000, 4000, 5000)
	arr := []int64{1000, 2000, 4500, 4600, 5000}
	got := Hold(fs, arr, 2000, 5000, 0)
	if got == nil || got.Frame.Timestamp != 3000 {
		t.Fatalf("want ts 3000, got %v", got)
	}
}
