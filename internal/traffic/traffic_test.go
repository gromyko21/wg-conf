package traffic

import "testing"

func TestMonthBounds(t *testing.T) {
	start, end, err := MonthBounds("2026-07")
	if err != nil {
		t.Fatal(err)
	}
	if start.Day() != 1 || start.Month() != 7 {
		t.Fatalf("unexpected start: %v", start)
	}
	if end.Month() != 8 {
		t.Fatalf("unexpected end: %v", end)
	}
}

func TestPreviousMonthKey(t *testing.T) {
	prev, err := PreviousMonthKey("2026-07")
	if err != nil || prev != "2026-06" {
		t.Fatalf("expected 2026-06, got %q err=%v", prev, err)
	}
}
