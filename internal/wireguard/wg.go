package wireguard

import (
	"bytes"
	"fmt"
	"os/exec"
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
	strip.Env = append(strip.Environ(), "WG_CONFIG_FILE="+confPath)
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
	return nil
}

// PeerStats holds runtime statistics for a peer.
type PeerStats struct {
	PublicKey      string
	Endpoint       string
	ReceiveBytes   int64
	TransmitBytes  int64
	LastHandshake  time.Time
	Online         bool
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
