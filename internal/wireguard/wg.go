package wireguard

import (
	"bytes"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// Client wraps wgctrl and shell commands for WireGuard operations.
type Client struct {
	ctrl *wgctrl.Client
}

func New() (*Client, error) {
	ctrl, err := wgctrl.New()
	if err != nil {
		return nil, fmt.Errorf("wgctrl init: %w", err)
	}
	return &Client{ctrl: ctrl}, nil
}

func (c *Client) Close() error {
	return c.ctrl.Close()
}

// SyncConf applies config changes without restarting the interface.
func SyncConf(iface, confPath string) error {
	strip := exec.Command("wg-quick", "strip", iface)
	var stripped bytes.Buffer
	strip.Stdout = &stripped
	if err := strip.Run(); err != nil {
		return fmt.Errorf("wg-quick strip: %w", err)
	}

	sync := exec.Command("wg", "syncconf", iface)
	sync.Stdin = &stripped
	if out, err := sync.CombinedOutput(); err != nil {
		return fmt.Errorf("wg syncconf: %w: %s", err, string(out))
	}
	_ = confPath
	return nil
}

// AddPeer applies a peer to the live interface.
func (c *Client) AddPeer(iface, publicKey, presharedKey, allowedIPs string) error {
	pub, err := wgtypes.ParseKey(publicKey)
	if err != nil {
		return fmt.Errorf("parse public key: %w", err)
	}

	peer := wgtypes.PeerConfig{
		PublicKey:  pub,
		AllowedIPs: nil,
	}
	if presharedKey != "" {
		psk, err := wgtypes.ParseKey(presharedKey)
		if err != nil {
			return fmt.Errorf("parse preshared key: %w", err)
		}
		peer.PresharedKey = &psk
	}

	ips, err := parseAllowedIPs(allowedIPs)
	if err != nil {
		return err
	}
	peer.AllowedIPs = ips

	if err := c.ctrl.ConfigureDevice(iface, wgtypes.Config{Peers: []wgtypes.PeerConfig{peer}}); err != nil {
		return fmt.Errorf("configure device: %w", err)
	}
	return nil
}

// RemovePeer removes a peer from the live interface. Ignores already removed peers.
func (c *Client) RemovePeer(iface, publicKey string) error {
	pub, err := wgtypes.ParseKey(publicKey)
	if err != nil {
		return fmt.Errorf("parse public key: %w", err)
	}

	dev, err := c.ctrl.Device(iface)
	if err != nil {
		return fmt.Errorf("get device %s: %w", iface, err)
	}
	for _, p := range dev.Peers {
		if p.PublicKey == pub {
			if err := c.ctrl.ConfigureDevice(iface, wgtypes.Config{
				Peers: []wgtypes.PeerConfig{{
					PublicKey: pub,
					Remove:    true,
				}},
			}); err != nil {
				return fmt.Errorf("configure device remove peer: %w", err)
			}
			return nil
		}
	}
	return nil
}

func parseAllowedIPs(raw string) ([]net.IPNet, error) {
	parts := strings.Split(raw, ",")
	out := make([]net.IPNet, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		_, ipnet, err := net.ParseCIDR(part)
		if err != nil {
			return nil, fmt.Errorf("parse allowed ip %q: %w", part, err)
		}
		out = append(out, *ipnet)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no allowed ips provided")
	}
	return out, nil
}

// PeerStats holds runtime statistics for a peer.
type PeerStats struct {
	PublicKey     string
	Endpoint      string
	ReceiveBytes  int64
	TransmitBytes int64
	LastHandshake time.Time
	Online        bool
}

const onlineThreshold = 3 * time.Minute

// GetPeerStats returns transfer and handshake data for all peers on the interface.
func (c *Client) GetPeerStats(iface string) ([]PeerStats, error) {
	dev, err := c.ctrl.Device(iface)
	if err != nil {
		return nil, fmt.Errorf("get device %s: %w", iface, err)
	}

	now := time.Now()
	stats := make([]PeerStats, 0, len(dev.Peers))
	for _, p := range dev.Peers {
		online := !p.LastHandshakeTime.IsZero() && now.Sub(p.LastHandshakeTime) < onlineThreshold
		endpoint := ""
		if p.Endpoint != nil {
			endpoint = p.Endpoint.String()
		}
		stats = append(stats, PeerStats{
			PublicKey:     p.PublicKey.String(),
			Endpoint:      endpoint,
			ReceiveBytes:  int64(p.ReceiveBytes),
			TransmitBytes: int64(p.TransmitBytes),
			LastHandshake: p.LastHandshakeTime,
			Online:        online,
		})
	}
	return stats, nil
}

// ParsePublicKey parses a base64 WireGuard public key.
func ParsePublicKey(s string) (wgtypes.Key, error) {
	return wgtypes.ParseKey(s)
}
