package monitor

import (
	"context"
	"log/slog"
	"time"

	"github.com/user/wg-conf/internal/config"
	"github.com/user/wg-conf/internal/store"
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
	for _, p := range peers {
		s, ok := byKey[p.PublicKey]
		if !ok {
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
