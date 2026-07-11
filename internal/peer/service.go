package peer

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/user/wg-conf/internal/clientfile"
	"github.com/user/wg-conf/internal/config"
	"github.com/user/wg-conf/internal/keys"
	"github.com/user/wg-conf/internal/store"
	"github.com/user/wg-conf/internal/traffic"
	"github.com/user/wg-conf/internal/wgconf"
	"github.com/user/wg-conf/internal/wireguard"
)

var nameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,15}$`)

var (
	ErrPeerExists    = errors.New("peer already exists")
	ErrPeerNotFound  = errors.New("peer not found")
	ErrConfigMissing = errors.New("client config not found (peer was created outside wg-conf)")
	ErrInvalidName   = errors.New("invalid peer name")
	ErrNoFreeIP      = errors.New("no free IP addresses in subnet")
)

type Service struct {
	params      *config.ServerParams
	wgDir       string
	clientsDirs []string
	store       *store.Store
	wg          *wireguard.Client
}

type CreateResult struct {
	Name         string    `json:"name"`
	PublicKey    string    `json:"public_key"`
	IPv4         string    `json:"ipv4"`
	IPv6         string    `json:"ipv6"`
	Config       string    `json:"config"`
	CreatedAt    time.Time `json:"created_at"`
}

type PeerView struct {
	Name               string    `json:"name"`
	PublicKey          string    `json:"public_key"`
	IPv4               string    `json:"ipv4"`
	IPv6               string    `json:"ipv6"`
	Online             bool      `json:"online"`
	LastActivity       time.Time `json:"last_activity,omitempty"`
	MonthUploadBytes   int64     `json:"month_upload_bytes"`
	MonthDownloadBytes int64     `json:"month_download_bytes"`
	MonthTotalBytes    int64     `json:"month_total_bytes"`
	MonthlyLimitBytes  int64     `json:"monthly_limit_bytes"`
	LimitExceeded      bool      `json:"limit_exceeded"`
	MonthLabel         string    `json:"month_label"`
	CreatedAt          time.Time `json:"created_at,omitempty"`
	CreatedBy          string    `json:"created_by,omitempty"`
	Enabled            bool      `json:"enabled"`
}

func NewService(params *config.ServerParams, wgDir string, clientsDirs []string, st *store.Store, wg *wireguard.Client) *Service {
	return &Service{
		params:      params,
		wgDir:       wgDir,
		clientsDirs: clientsDirs,
		store:       st,
		wg:          wg,
	}
}

func (s *Service) SyncFromConfig(ctx context.Context) error {
	confPath := s.params.WGConfPath(s.wgDir)
	peers, err := wgconf.Parse(confPath)
	if err != nil {
		return err
	}

	records := make([]store.PeerRecord, len(peers))
	names := make([]string, len(peers))
	for i, p := range peers {
		cfg, _ := clientfile.Load(s.clientsDirs, s.params.ServerWGNIC, p.Name)
		if cfg != "" {
			if updated, _, err := wgconf.EnsureJunkParams(cfg); err == nil {
				cfg = updated
			}
		}
		names[i] = p.Name
		enabled := true
		if existing, err := s.store.GetPeer(ctx, p.Name); err == nil && existing != nil {
			enabled = existing.Enabled
		}
		records[i] = store.PeerRecord{
			Name:         p.Name,
			PublicKey:    p.PublicKey,
			IPv4:         p.IPv4,
			IPv6:         p.IPv6,
			Enabled:      enabled,
			CreatedAt:    time.Now().UTC(),
			ClientConfig: cfg,
		}
	}
	if err := s.store.SyncPeersFromConfig(ctx, records); err != nil {
		return err
	}
	return s.store.PrunePeersExcept(ctx, names)
}

func (s *Service) List(ctx context.Context) ([]PeerView, error) {
	if err := s.SyncFromConfig(ctx); err != nil {
		return nil, err
	}

	dbPeers, err := s.store.ListPeers(ctx)
	if err != nil {
		return nil, err
	}

	usage, err := s.store.LatestUsageByPeer(ctx)
	if err != nil {
		return nil, err
	}

	month := traffic.MonthKey(time.Now())
	monthly, err := s.store.MonthlyTrafficByPeer(ctx, month)
	if err != nil {
		return nil, err
	}

	views := make([]PeerView, 0, len(dbPeers))
	for _, p := range dbPeers {
		v := PeerView{
			Name:              p.Name,
			PublicKey:         p.PublicKey,
			IPv4:              p.IPv4,
			IPv6:              p.IPv6,
			Enabled:           p.Enabled,
			CreatedAt:         p.CreatedAt,
			CreatedBy:         p.CreatedBy,
			MonthlyLimitBytes: p.MonthlyLimitBytes,
			MonthLabel:        month,
		}
		if u, ok := usage[p.Name]; ok && p.Enabled {
			v.Online = u.Online
			v.LastActivity = u.LastHandshake
		}
		if m, ok := monthly[p.Name]; ok {
			v.MonthUploadBytes = m.UploadBytes
			v.MonthDownloadBytes = m.DownloadBytes
			v.MonthTotalBytes = m.TotalBytes
			v.LimitExceeded = m.LimitExceeded
		}
		if v.MonthlyLimitBytes > 0 && v.MonthTotalBytes >= v.MonthlyLimitBytes {
			v.LimitExceeded = true
		}
		views = append(views, v)
	}
	return views, nil
}

func (s *Service) SetLimit(ctx context.Context, name string, limitGB float64, actor string) error {
	rec, err := s.store.GetPeer(ctx, name)
	if err != nil {
		return err
	}
	if rec == nil {
		return ErrPeerNotFound
	}

	var limitBytes int64
	if limitGB > 0 {
		limitBytes = int64(limitGB * 1024 * 1024 * 1024)
	}
	if err := s.store.SetPeerLimit(ctx, name, limitBytes); err != nil {
		return err
	}
	details := "unlimited"
	if limitBytes > 0 {
		details = fmt.Sprintf("%.2f GB", limitGB)
	}
	return s.store.AddAudit(ctx, actor, "set_limit", name, details)
}

func (s *Service) Create(ctx context.Context, name, actor string) (*CreateResult, error) {
	if !nameRe.MatchString(name) {
		return nil, ErrInvalidName
	}

	confPath := s.params.WGConfPath(s.wgDir)
	existing, err := wgconf.Parse(confPath)
	if err != nil {
		return nil, err
	}
	for _, p := range existing {
		if p.Name == name {
			return nil, ErrPeerExists
		}
	}

	usedIPs := make([]string, len(existing))
	for i, p := range existing {
		usedIPs[i] = p.IPv4
	}

	dotIP, err := wgconf.FindFreeIP(s.params.ServerWGIPv4, usedIPs)
	if err != nil {
		return nil, ErrNoFreeIP
	}

	baseIPv4 := s.params.ServerWGIPv4[:lastDot(s.params.ServerWGIPv4)]
	clientIPv4 := fmt.Sprintf("%s.%d", baseIPv4, dotIP)

	baseIPv6 := splitIPv6Base(s.params.ServerWGIPv6)
	clientIPv6 := fmt.Sprintf("%s::%d", baseIPv6, dotIP)

	keyPair, err := keys.GeneratePair()
	if err != nil {
		return nil, err
	}
	psk, err := keys.GeneratePSK()
	if err != nil {
		return nil, err
	}

	junk, err := wgconf.GenerateJunkParams()
	if err != nil {
		return nil, err
	}

	allowedIPs := fmt.Sprintf("%s/32,%s/128", clientIPv4, clientIPv6)
	peer := wgconf.Peer{
		Name:       name,
		PublicKey:  keyPair.Public,
		Preshared:  psk,
		AllowedIPs: allowedIPs,
		IPv4:       clientIPv4,
		IPv6:       clientIPv6,
	}

	if err := wgconf.AppendPeer(confPath, peer); err != nil {
		return nil, err
	}

	if err := s.wg.AddPeer(s.params.ServerWGNIC, keyPair.Public, psk, allowedIPs); err != nil {
		_ = wgconf.RemovePeer(confPath, name)
		return nil, err
	}

	clientConfig := wgconf.BuildClientConfig(
		keyPair.Private,
		clientIPv4,
		clientIPv6,
		s.params.ClientDNS1,
		s.params.ClientDNS2,
		s.params.ServerPubKey,
		psk,
		s.params.Endpoint(),
		s.params.AllowedIPs,
		junk,
	)

	now := time.Now().UTC()
	rec := store.PeerRecord{
		Name:         name,
		PublicKey:    keyPair.Public,
		IPv4:         clientIPv4,
		IPv6:         clientIPv6,
		Enabled:      true,
		CreatedAt:    now,
		CreatedBy:    actor,
		ClientConfig: clientConfig,
	}
	if err := s.store.UpsertPeer(ctx, rec); err != nil {
		return nil, err
	}
	_ = s.store.AddAudit(ctx, actor, "create", name, clientIPv4)

	for _, dir := range s.clientsDirs {
		_ = clientfile.Save(dir, s.params.ServerWGNIC, name, clientConfig)
	}

	return &CreateResult{
		Name:      name,
		PublicKey: keyPair.Public,
		IPv4:      clientIPv4,
		IPv6:      clientIPv6,
		Config:    clientConfig,
		CreatedAt: now,
	}, nil
}

func (s *Service) GetConfig(ctx context.Context, name string) (string, error) {
	rec, err := s.store.GetPeer(ctx, name)
	if err != nil {
		return "", err
	}

	var cfg string
	if rec != nil && rec.ClientConfig != "" {
		cfg = rec.ClientConfig
	} else {
		loaded, loadErr := clientfile.Load(s.clientsDirs, s.params.ServerWGNIC, name)
		if loadErr == nil {
			cfg = loaded
		}
	}

	if cfg != "" {
		updated, _, err := wgconf.EnsureJunkParams(cfg)
		if err != nil {
			return "", err
		}
		if updated != cfg && rec != nil {
			_ = s.store.UpsertPeer(ctx, store.PeerRecord{
				Name:         rec.Name,
				PublicKey:    rec.PublicKey,
				IPv4:         rec.IPv4,
				IPv6:         rec.IPv6,
				Enabled:      rec.Enabled,
				CreatedAt:    rec.CreatedAt,
				CreatedBy:    rec.CreatedBy,
				ClientConfig: updated,
			})
		} else if updated != cfg {
			_ = s.store.UpsertPeer(ctx, store.PeerRecord{
				Name:         name,
				ClientConfig: updated,
				Enabled:      true,
				CreatedAt:    time.Now().UTC(),
				CreatedBy:    "import",
			})
		}
		for _, dir := range s.clientsDirs {
			_ = clientfile.Save(dir, s.params.ServerWGNIC, name, updated)
		}
		return updated, nil
	}

	confPath := s.params.WGConfPath(s.wgDir)
	peers, parseErr := wgconf.Parse(confPath)
	if parseErr != nil {
		return "", parseErr
	}
	for _, p := range peers {
		if p.Name == name {
			return "", ErrConfigMissing
		}
	}
	return "", ErrPeerNotFound
}

func (s *Service) Revoke(ctx context.Context, name, actor string) error {
	confPath := s.params.WGConfPath(s.wgDir)
	confPeers, err := wgconf.Parse(confPath)
	if err != nil {
		return err
	}

	var publicKey string
	inConf := false
	for _, p := range confPeers {
		if p.Name == name {
			inConf = true
			publicKey = p.PublicKey
			break
		}
	}

	rec, err := s.store.GetPeer(ctx, name)
	if err != nil {
		return err
	}
	if !inConf && rec == nil {
		return ErrPeerNotFound
	}
	if publicKey == "" && rec != nil {
		publicKey = rec.PublicKey
	}

	if inConf {
		if err := wgconf.RemovePeer(confPath, name); err != nil {
			return err
		}
	}
	if publicKey != "" {
		if err := s.wg.RemovePeer(s.params.ServerWGNIC, publicKey); err != nil {
			return err
		}
	}

	if err := s.store.DeletePeer(ctx, name); err != nil {
		return err
	}
	clientfile.Remove(s.clientsDirs, s.params.ServerWGNIC, name)
	return s.store.AddAudit(ctx, actor, "revoke", name, "")
}

func (s *Service) Stop(ctx context.Context, name, actor string) error {
	if err := s.SyncFromConfig(ctx); err != nil {
		return err
	}

	confPeer, err := s.findConfigPeer(name)
	if err != nil {
		return err
	}

	rec, err := s.store.GetPeer(ctx, name)
	if err != nil {
		return err
	}
	if rec != nil && !rec.Enabled {
		return nil
	}

	publicKey := confPeer.PublicKey
	if publicKey == "" && rec != nil {
		publicKey = rec.PublicKey
	}
	if publicKey == "" {
		return ErrPeerNotFound
	}

	if err := s.wg.RemovePeer(s.params.ServerWGNIC, publicKey); err != nil {
		return err
	}
	if err := s.store.SetPeerEnabled(ctx, name, false); err != nil {
		return err
	}
	return s.store.AddAudit(ctx, actor, "stop", name, "")
}

func (s *Service) Start(ctx context.Context, name, actor string) error {
	if err := s.SyncFromConfig(ctx); err != nil {
		return err
	}

	confPeer, err := s.findConfigPeer(name)
	if err != nil {
		return err
	}

	rec, err := s.store.GetPeer(ctx, name)
	if err != nil {
		return err
	}
	if rec != nil && rec.Enabled {
		return nil
	}

	if err := s.wg.AddPeer(s.params.ServerWGNIC, confPeer.PublicKey, confPeer.Preshared, confPeer.AllowedIPs); err != nil {
		return err
	}
	if err := s.store.SetPeerEnabled(ctx, name, true); err != nil {
		return err
	}
	return s.store.AddAudit(ctx, actor, "start", name, "")
}

func (s *Service) ApplyDisabledPeers(ctx context.Context) error {
	peers, err := s.store.ListPeers(ctx)
	if err != nil {
		return err
	}
	for _, p := range peers {
		if p.Enabled || p.PublicKey == "" {
			continue
		}
		if err := s.wg.RemovePeer(s.params.ServerWGNIC, p.PublicKey); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) findConfigPeer(name string) (*wgconf.Peer, error) {
	confPath := s.params.WGConfPath(s.wgDir)
	peers, err := wgconf.Parse(confPath)
	if err != nil {
		return nil, err
	}
	for _, p := range peers {
		if p.Name == name {
			return &p, nil
		}
	}
	return nil, ErrPeerNotFound
}

func lastDot(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '.' {
			return i
		}
	}
	return -1
}

func splitIPv6Base(s string) string {
	parts := strings.Split(s, "::")
	if len(parts) > 0 && parts[0] != "" {
		return parts[0]
	}
	return s
}
