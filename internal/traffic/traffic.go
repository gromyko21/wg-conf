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

func MonthBounds(monthKey string) (start, end time.Time, err error) {
	t, err := time.Parse("2006-01", monthKey)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	start = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	end = start.AddDate(0, 1, 0)
	return start, end, nil
}

func PreviousMonthKey(monthKey string) (string, error) {
	start, _, err := MonthBounds(monthKey)
	if err != nil {
		return "", err
	}
	return MonthKey(start.AddDate(0, -1, 0)), nil
}
