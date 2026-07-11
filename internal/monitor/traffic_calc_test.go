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

func TestComputeMonthTraffic_BaselineZeroCounterReset(t *testing.T) {
	got := computeMonthTraffic(monthTrafficInput{
		BaselineRx:     0,
		BaselineTx:     0,
		CurrentRx:      0,
		CurrentTx:      0,
		PrevRx:         5_000_000,
		PrevTx:         8_000_000,
		HasPrev:        true,
		StoredUpload:   5_000_000,
		StoredDownload: 8_000_000,
		HasStored:      true,
	})
	if got.Upload != 5_000_000 || got.Download != 8_000_000 || !got.ReanchorBaseline {
		t.Fatalf("unexpected result: %+v", got)
	}
	if got.UploadOffset != 5_000_000 || got.DownloadOffset != 8_000_000 {
		t.Fatalf("unexpected offsets: %+v", got)
	}
}

func TestComputeMonthTraffic_BaselineZeroNoFalseResetOnGrowth(t *testing.T) {
	got := computeMonthTraffic(monthTrafficInput{
		BaselineRx: 0,
		BaselineTx: 0,
		CurrentRx:  1000,
		CurrentTx:  2000,
		PrevRx:     500,
		PrevTx:     800,
		HasPrev:    true,
	})
	if got.Upload != 1000 || got.Download != 2000 || got.ReanchorBaseline {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestComputeMonthTraffic_CounterResetContinuesGrowing(t *testing.T) {
	first := computeMonthTraffic(monthTrafficInput{
		BaselineRx: 0,
		BaselineTx: 0,
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
	if second.Upload != 1_500_000 || second.Download != 2_300_000 || second.ReanchorBaseline {
		t.Fatalf("unexpected second result: %+v", second)
	}
}

func TestComputeMonthTraffic_CounterResetUsesStoredFallback(t *testing.T) {
	got := computeMonthTraffic(monthTrafficInput{
		BaselineRx:     1000,
		BaselineTx:     2000,
		CurrentRx:      0,
		CurrentTx:      0,
		StoredUpload:   4_000_000,
		StoredDownload: 6_000_000,
		HasStored:      true,
	})
	if got.Upload != 4_000_000 || got.Download != 6_000_000 || !got.ReanchorBaseline {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestComputeMonthTraffic_SecondRestartPreservesOffset(t *testing.T) {
	first := computeMonthTraffic(monthTrafficInput{
		BaselineRx: 0,
		BaselineTx: 0,
		CurrentRx:  0,
		CurrentTx:  0,
		PrevRx:     5_000_000,
		PrevTx:     8_000_000,
		HasPrev:    true,
	})

	second := computeMonthTraffic(monthTrafficInput{
		BaselineRx:     first.BaselineRx,
		BaselineTx:     first.BaselineTx,
		UploadOffset:   first.UploadOffset,
		DownloadOffset: first.DownloadOffset,
		CurrentRx:      200_000,
		CurrentTx:      100_000,
	})

	third := computeMonthTraffic(monthTrafficInput{
		BaselineRx:     second.BaselineRx,
		BaselineTx:     second.BaselineTx,
		UploadOffset:   second.UploadOffset,
		DownloadOffset: second.DownloadOffset,
		CurrentRx:      0,
		CurrentTx:      0,
		PrevRx:         200_000,
		PrevTx:         100_000,
		HasPrev:        true,
		StoredUpload:   second.Upload,
		StoredDownload: second.Download,
		HasStored:      true,
	})

	if third.Upload != 5_200_000 || third.Download != 8_100_000 {
		t.Fatalf("unexpected third result: %+v", third)
	}
}
