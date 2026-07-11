package traffic

import "testing"

func TestDelta(t *testing.T) {
	if got := ClientUploadDelta(100, 150); got != 50 {
		t.Fatalf("expected 50, got %d", got)
	}
	if got := ClientUploadDelta(150, 100); got != 100 {
		t.Fatalf("expected reset delta 100, got %d", got)
	}
}
