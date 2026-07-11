package monitor

import "github.com/user/wg-conf/internal/traffic"

type monthTrafficInput struct {
	BaselineRx, BaselineTx       int64
	UploadOffset, DownloadOffset int64
	CurrentRx, CurrentTx         int64
	PrevRx, PrevTx               int64
	HasPrev                      bool
	StoredUpload, StoredDownload int64
	HasStored                    bool
}

type monthTrafficResult struct {
	Upload, Download             int64
	BaselineRx, BaselineTx       int64
	UploadOffset, DownloadOffset int64
	ReanchorBaseline             bool
}

func computeMonthTraffic(in monthTrafficInput) monthTrafficResult {
	counterReset := countersDropped(in)
	if !counterReset {
		return monthTrafficResult{
			Upload:         in.UploadOffset + traffic.ClientUploadDelta(in.BaselineRx, in.CurrentRx),
			Download:       in.DownloadOffset + traffic.ClientDownloadDelta(in.BaselineTx, in.CurrentTx),
			BaselineRx:     in.BaselineRx,
			BaselineTx:     in.BaselineTx,
			UploadOffset:   in.UploadOffset,
			DownloadOffset: in.DownloadOffset,
		}
	}

	segmentUpload := segmentBeforeReset(in.BaselineRx, in.PrevRx, in.HasPrev)
	segmentDownload := segmentBeforeReset(in.BaselineTx, in.PrevTx, in.HasPrev)

	newUploadOffset := in.UploadOffset + segmentUpload
	newDownloadOffset := in.DownloadOffset + segmentDownload

	if newUploadOffset == in.UploadOffset && in.HasStored && in.StoredUpload > newUploadOffset {
		newUploadOffset = in.StoredUpload
	}
	if newDownloadOffset == in.DownloadOffset && in.HasStored && in.StoredDownload > newDownloadOffset {
		newDownloadOffset = in.StoredDownload
	}

	return monthTrafficResult{
		Upload:           newUploadOffset + traffic.ClientUploadDelta(0, in.CurrentRx),
		Download:         newDownloadOffset + traffic.ClientDownloadDelta(0, in.CurrentTx),
		BaselineRx:       0,
		BaselineTx:       0,
		UploadOffset:     newUploadOffset,
		DownloadOffset:   newDownloadOffset,
		ReanchorBaseline: true,
	}
}

func countersDropped(in monthTrafficInput) bool {
	if in.HasPrev && (in.CurrentRx < in.PrevRx || in.CurrentTx < in.PrevTx) {
		return true
	}
	return in.CurrentRx < in.BaselineRx || in.CurrentTx < in.BaselineTx
}

func segmentBeforeReset(baseline, prev int64, hasPrev bool) int64 {
	if hasPrev && prev >= baseline {
		return traffic.ClientUploadDelta(baseline, prev)
	}
	return 0
}
