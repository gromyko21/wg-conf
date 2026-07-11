package keys

import (
	"fmt"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// Pair holds a WireGuard private/public key pair.
type Pair struct {
	Private string
	Public  string
}

func GeneratePair() (Pair, error) {
	priv, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return Pair{}, fmt.Errorf("generate private key: %w", err)
	}
	return Pair{
		Private: priv.String(),
		Public:  priv.PublicKey().String(),
	}, nil
}

func GeneratePSK() (string, error) {
	key, err := wgtypes.GenerateKey()
	if err != nil {
		return "", fmt.Errorf("generate preshared key: %w", err)
	}
	return key.String(), nil
}
