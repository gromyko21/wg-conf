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
	Name          string    `json:"name"`
	PublicKey     string    `json:"public_key"`
	IPv4          string    `json:"ipv4"`
	IPv6          string    `json:"ipv6"`
	Online        bool      `json:"online"`
	RxBytes       int64     `json:"rx_bytes"`
	TxBytes       int64     `json:"tx_bytes"`
	LastHandshake time.Time `json:"last_handshake,omitempty"`
	Endpoint      string    `json:"endpoint,omitempty"`
	CreatedAt     time.Time `json:"created_at,omitempty"`
	CreatedBy     string    `json:"created_by,omitempty"`
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
	for i, p := range peers {
		cfg, _ := clientfile.Load(s.clientsDirs, s.params.ServerWGNIC, p.Name)
		if cfg != "" {
			if updated, _, err := wgconf.EnsureJunkParams(cfg); err == nil {
				cfg = updated
			}
		}
		records[i] = store.PeerRecord{
			Name:         p.Name,
			PublicKey:    p.PublicKey,
			IPv4:         p.IPv4,
			IPv6:         p.IPv6,
			Enabled:      true,
			CreatedAt:    time.Now().UTC(),
			ClientConfig: cfg,
		}
	}
	return s.store.SyncPeersFromConfig(ctx, records)
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

	views := make([]PeerView, 0, len(dbPeers))
	for _, p := range dbPeers {
		v := PeerView{
			Name:      p.Name,
			PublicKey: p.PublicKey,
			IPv4:      p.IPv4,
			IPv6:      p.IPv6,
			CreatedAt: p.CreatedAt,
			CreatedBy: p.CreatedBy,
		}
		if u, ok := usage[p.Name]; ok {
			v.Online = u.Online
			v.RxBytes = u.RxBytes
			v.TxBytes = u.TxBytes
			v.LastHandshake = u.LastHandshake
			v.Endpoint = u.Endpoint
		}
		views = append(views, v)
	}
	return views, nil
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

	if err := wireguard.AddPeer(s.params.ServerWGNIC, keyPair.Public, psk, allowedIPs); err != nil {
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
	peers, err := wgconf.Parse(confPath)
	if err != nil {
		return err
	}

	found := false
	var publicKey string
	for _, p := range peers {
		if p.Name == name {
			found = true
			publicKey = p.PublicKey
			break
		}
	}
	if !found {
		return ErrPeerNotFound
	}

	if err := wgconf.RemovePeer(confPath, name); err != nil {
		return err
	}
	if err := wireguard.RemovePeer(s.params.ServerWGNIC, publicKey); err != nil {
		return err
	}
	if err := s.store.DeletePeer(ctx, name); err != nil {
		return err
	}
	clientfile.Remove(s.clientsDirs, s.params.ServerWGNIC, name)
	return s.store.AddAudit(ctx, actor, "revoke", name, "")
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
