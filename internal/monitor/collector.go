package monitor

import (
	"context"
	"log/slog"
	"time"

	"github.com/user/wg-conf/internal/config"
	"github.com/user/wg-conf/internal/store"
	"github.com/user/wg-conf/internal/traffic"
	"github.com/user/wg-conf/internal/wgconf"
	"github.com/user/wg-conf/internal/wireguard"
)

type Collector struct {
	params   *config.ServerParams
	wgDir    string
	store    *store.Store
	wg       *wireguard.Client
	interval time.Duration
}

func New(params *config.ServerParams, wgDir string, st *store.Store, wg *wireguard.Client, interval time.Duration) *Collector {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &Collector{
		params:   params,
		wgDir:    wgDir,
		store:    st,
		wg:       wg,
		interval: interval,
	}
}

func (c *Collector) Run(ctx context.Context) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	c.collect(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.collect(ctx)
		}
	}
}

func (c *Collector) collect(ctx context.Context) {
	stats, err := c.wg.GetPeerStats(c.params.ServerWGNIC)
	if err != nil {
		slog.Error("collect peer stats", "error", err)
		return
	}

	byKey := make(map[string]wireguard.PeerStats, len(stats))
	for _, s := range stats {
		byKey[s.PublicKey] = s
	}

	peers, err := wgconf.Parse(c.params.WGConfPath(c.wgDir))
	if err != nil {
		slog.Error("parse wg config", "error", err)
		return
	}

	now := time.Now().UTC()
	month := traffic.MonthKey(now)
	prevMonth, err := traffic.PreviousMonthKey(month)
	if err != nil {
		slog.Error("previous month", "error", err)
		return
	}

	for _, p := range peers {
		s, ok := byKey[p.PublicKey]
		if !ok {
			continue
		}

		baseline, hasBaseline, err := c.store.GetTrafficBaseline(ctx, p.Name, month)
		if err != nil {
			slog.Error("load traffic baseline", "peer", p.Name, "error", err)
			continue
		}
		if !hasBaseline {
			rx, tx := c.resolveBaseline(ctx, p.Name, prevMonth, s.ReceiveBytes, s.TransmitBytes)
			baseline = store.TrafficBaseline{RxBytes: rx, TxBytes: tx}
			if err := c.store.SetTrafficBaseline(ctx, p.Name, month, baseline); err != nil {
				slog.Error("save traffic baseline", "peer", p.Name, "error", err)
				continue
			}
		}

		var prevSnap store.UsageSnapshot
		hasPrevSnap := false
		if snap, found, snapErr := c.store.GetLatestUsageSnapshot(ctx, p.Name); snapErr != nil {
			slog.Error("load previous usage snapshot", "peer", p.Name, "error", snapErr)
			continue
		} else if found {
			prevSnap = snap
			hasPrevSnap = true
		}

		result := computeMonthTraffic(monthTrafficInput{
			BaselineRx:     baseline.RxBytes,
			BaselineTx:     baseline.TxBytes,
			UploadOffset:   baseline.UploadOffset,
			DownloadOffset: baseline.DownloadOffset,
			CurrentRx:      s.ReceiveBytes,
			CurrentTx:      s.TransmitBytes,
			PrevRx:         prevSnap.RxBytes,
			PrevTx:         prevSnap.TxBytes,
			HasPrev:        hasPrevSnap,
		})
		if result.ReanchorBaseline {
			if err := c.store.SetTrafficBaseline(ctx, p.Name, month, store.TrafficBaseline{
				RxBytes:        result.BaselineRx,
				TxBytes:        result.BaselineTx,
				UploadOffset:   result.UploadOffset,
				DownloadOffset: result.DownloadOffset,
			}); err != nil {
				slog.Error("reanchor traffic baseline", "peer", p.Name, "error", err)
				continue
			}
			slog.Info("traffic counter reset detected", "peer", p.Name, "upload", result.Upload, "download", result.Download)
		}

		if err := c.store.SetMonthlyTraffic(ctx, p.Name, month, result.Upload, result.Download); err != nil {
			slog.Error("set monthly traffic", "peer", p.Name, "error", err)
			continue
		}

		snap := store.UsageSnapshot{
			PeerName:      p.Name,
			PublicKey:     p.PublicKey,
			RxBytes:       s.ReceiveBytes,
			TxBytes:       s.TransmitBytes,
			LastHandshake: s.LastHandshake,
			Online:        s.Online,
			Endpoint:      s.Endpoint,
			CollectedAt:   now,
		}
		if err := c.store.SaveUsageSnapshot(ctx, snap); err != nil {
			slog.Error("save usage snapshot", "peer", p.Name, "error", err)
		}
	}
}

func (c *Collector) resolveBaseline(
	ctx context.Context,
	peerName, prevMonth string,
	currentRx, currentTx int64,
) (rx, tx int64) {
	if hadPrev, err := c.store.HasTrafficInMonth(ctx, peerName, prevMonth); err == nil && hadPrev {
		// New calendar month: start from counters at the first collect of the month.
		return currentRx, currentTx
	}
	// First tracking: import current WireGuard totals (best effort for mid-month start).
	return 0, 0
}
