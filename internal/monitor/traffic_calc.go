package monitor

import "github.com/user/wg-conf/internal/traffic"

type monthTrafficInput struct {
	BaselineRx, BaselineTx       int64
	UploadOffset, DownloadOffset int64
	CurrentRx, CurrentTx         int64
	PrevRx, PrevTx               int64
	HasPrev                      bool
}

type monthTrafficResult struct {
	Upload, Download             int64
	BaselineRx, BaselineTx       int64
	UploadOffset, DownloadOffset int64
	ReanchorBaseline             bool
}

func computeMonthTraffic(in monthTrafficInput) monthTrafficResult {
	counterReset := in.CurrentRx < in.BaselineRx || in.CurrentTx < in.BaselineTx
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

	segmentUpload := traffic.ClientUploadDelta(in.BaselineRx, in.PrevRx)
	if !in.HasPrev || in.PrevRx < in.BaselineRx {
		segmentUpload = 0
	}
	segmentDownload := traffic.ClientDownloadDelta(in.BaselineTx, in.PrevTx)
	if !in.HasPrev || in.PrevTx < in.BaselineTx {
		segmentDownload = 0
	}

	newUploadOffset := in.UploadOffset + segmentUpload
	newDownloadOffset := in.DownloadOffset + segmentDownload

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
