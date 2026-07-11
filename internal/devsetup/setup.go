package devsetup

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/user/wg-conf/internal/keys"
	"github.com/user/wg-conf/internal/wgconf"
)

// Ensure creates local dev fixtures with freshly generated keys (never committed to git).
func Ensure(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	paramsPath := filepath.Join(dir, "params")
	confPath := filepath.Join(dir, "wg0.conf")

	if _, err := os.Stat(paramsPath); err == nil {
		if _, err := os.Stat(confPath); err == nil {
			return nil
		}
	}

	serverKeys, err := keys.GeneratePair()
	if err != nil {
		return fmt.Errorf("generate server keys: %w", err)
	}

	params := fmt.Sprintf(`SERVER_PUB_IP=127.0.0.1
SERVER_PUB_NIC=eth0
SERVER_WG_NIC=wg0
SERVER_WG_IPV4=10.66.66.1
SERVER_WG_IPV6=fd42:42:42::1
SERVER_PORT=51820
SERVER_PRIV_KEY=%s
SERVER_PUB_KEY=%s
CLIENT_DNS_1=1.1.1.1
CLIENT_DNS_2=1.0.0.1
ALLOWED_IPS=0.0.0.0/0,::/0
`, serverKeys.Private, serverKeys.Public)

	conf := fmt.Sprintf(`[Interface]
Address = 10.66.66.1/24,fd42:42:42::1/64
ListenPort = 51820
PrivateKey = %s
`, serverKeys.Private)

	if err := os.WriteFile(paramsPath, []byte(params), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(confPath, []byte(conf), 0o600); err != nil {
		return err
	}

	// Optional sample client for UI testing.
	clientKeys, err := keys.GeneratePair()
	if err != nil {
		return err
	}
	psk, err := keys.GeneratePSK()
	if err != nil {
		return err
	}
	junk, err := wgconf.GenerateJunkParams()
	if err != nil {
		return err
	}

	samplePeer := wgconf.Peer{
		Name:       "alice",
		PublicKey:  clientKeys.Public,
		Preshared:  psk,
		AllowedIPs: "10.66.66.2/32,fd42:42:42::2/128",
		IPv4:       "10.66.66.2",
		IPv6:       "fd42:42:42::2",
	}
	if err := wgconf.AppendPeer(confPath, samplePeer); err != nil {
		return err
	}

	clientCfg := wgconf.BuildClientConfig(
		clientKeys.Private,
		"10.66.66.2",
		"fd42:42:42::2",
		"1.1.1.1",
		"1.0.0.1",
		serverKeys.Public,
		psk,
		"127.0.0.1:51820",
		"0.0.0.0/0,::/0",
		junk,
	)
	clientPath := filepath.Join(dir, "wg0-client-alice.conf")
	return os.WriteFile(clientPath, []byte(clientCfg), 0o600)
}
