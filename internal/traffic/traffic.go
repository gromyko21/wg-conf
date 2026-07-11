package traffic

import "time"

// ClientUploadDelta returns bytes the client uploaded since the last sample (server RX).
func ClientUploadDelta(prev, current int64) int64 {
	return delta(prev, current)
}

// ClientDownloadDelta returns bytes the client downloaded since the last sample (server TX).
func ClientDownloadDelta(prev, current int64) int64 {
	return delta(prev, current)
}

func delta(prev, current int64) int64 {
	if current >= prev {
		return current - prev
	}
	// WireGuard counters reset after peer/interface restart.
	return current
}

func MonthKey(t time.Time) string {
	return t.UTC().Format("2006-01")
}
