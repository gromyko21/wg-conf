package monitor

import "testing"

func TestComputeMonthTraffic_Normal(t *testing.T) {
	got := computeMonthTraffic(monthTrafficInput{
		BaselineRx: 1000,
		BaselineTx: 2000,
		CurrentRx:  5000,
		CurrentTx:  8000,
	})
	if got.Upload != 4000 || got.Download != 6000 || got.ReanchorBaseline {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestComputeMonthTraffic_CounterResetPreservesHistory(t *testing.T) {
	got := computeMonthTraffic(monthTrafficInput{
		BaselineRx: 1000,
		BaselineTx: 2000,
		CurrentRx:  0,
		CurrentTx:  0,
		PrevRx:     5_000_000,
		PrevTx:     8_000_000,
		HasPrev:    true,
	})
	if got.Upload != 4_999_000 || got.Download != 7_998_000 || !got.ReanchorBaseline {
		t.Fatalf("unexpected result: %+v", got)
	}
	if got.UploadOffset != 4_999_000 || got.DownloadOffset != 7_998_000 {
		t.Fatalf("unexpected offsets: %+v", got)
	}
}

func TestComputeMonthTraffic_CounterResetContinuesGrowing(t *testing.T) {
	first := computeMonthTraffic(monthTrafficInput{
		BaselineRx: 1000,
		BaselineTx: 2000,
		CurrentRx:  0,
		CurrentTx:  0,
		PrevRx:     1_000_000,
		PrevTx:     2_000_000,
		HasPrev:    true,
	})

	second := computeMonthTraffic(monthTrafficInput{
		BaselineRx:     first.BaselineRx,
		BaselineTx:     first.BaselineTx,
		UploadOffset:   first.UploadOffset,
		DownloadOffset: first.DownloadOffset,
		CurrentRx:      500_000,
		CurrentTx:      300_000,
	})
	if second.Upload != 1_499_000 || second.Download != 2_298_000 || second.ReanchorBaseline {
		t.Fatalf("unexpected second result: %+v", second)
	}
}

func TestComputeMonthTraffic_CounterResetWithPostRestartTraffic(t *testing.T) {
	got := computeMonthTraffic(monthTrafficInput{
		BaselineRx: 1000,
		BaselineTx: 2000,
		CurrentRx:  50,
		CurrentTx:  80,
		PrevRx:     5_000_000,
		PrevTx:     8_000_000,
		HasPrev:    true,
	})
	if got.Upload != 4_999_050 || got.Download != 7_998_080 {
		t.Fatalf("unexpected result: %+v", got)
	}
}
