package wgconf

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var clientNameRe = regexp.MustCompile(`^### Client ([a-zA-Z0-9_-]+)$`)

// Peer represents a client peer block in the server config.
type Peer struct {
	Name       string
	PublicKey  string
	Preshared  string
	AllowedIPs string
	IPv4       string
	IPv6       string
}

// Parse reads server WireGuard config and extracts client peers.
func Parse(path string) ([]Peer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return ParseContent(string(data))
}

func ParseContent(content string) ([]Peer, error) {
	var peers []Peer
	var current *Peer

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if m := clientNameRe.FindStringSubmatch(line); m != nil {
			if current != nil {
				peers = append(peers, *current)
			}
			current = &Peer{Name: m[1]}
			continue
		}

		if current == nil {
			continue
		}

		if strings.HasPrefix(line, "PublicKey = ") {
			current.PublicKey = strings.TrimPrefix(line, "PublicKey = ")
		} else if strings.HasPrefix(line, "PresharedKey = ") {
			current.Preshared = strings.TrimPrefix(line, "PresharedKey = ")
		} else if strings.HasPrefix(line, "AllowedIPs = ") {
			current.AllowedIPs = strings.TrimPrefix(line, "AllowedIPs = ")
			ipv4, ipv6 := splitAllowedIPs(current.AllowedIPs)
			current.IPv4 = ipv4
			current.IPv6 = ipv6
		}
	}

	if current != nil {
		peers = append(peers, *current)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return peers, nil
}

func splitAllowedIPs(allowed string) (ipv4, ipv6 string) {
	for _, part := range strings.Split(allowed, ",") {
		part = strings.TrimSpace(part)
		if strings.Contains(part, ".") {
			ipv4 = strings.TrimSuffix(part, "/32")
		} else if strings.Contains(part, ":") {
			ipv6 = strings.TrimSuffix(part, "/128")
		}
	}
	return
}

// FindFreeIP returns the first free host octet (2-254) in the server subnet.
func FindFreeIP(serverIPv4 string, used []string) (int, error) {
	base := serverIPv4[:strings.LastIndex(serverIPv4, ".")]
	usedSet := make(map[string]struct{}, len(used))
	for _, ip := range used {
		usedSet[ip] = struct{}{}
	}

	for i := 2; i <= 254; i++ {
		candidate := base + "." + strconv.Itoa(i)
		if _, ok := usedSet[candidate]; !ok {
			return i, nil
		}
	}
	return 0, fmt.Errorf("subnet supports only 253 clients, all IPs are taken")
}

// AppendPeer adds a new client peer block to the server config file.
func AppendPeer(path string, peer Peer) error {
	block := fmt.Sprintf(`
### Client %s
[Peer]
PublicKey = %s
PresharedKey = %s
AllowedIPs = %s
`, peer.Name, peer.PublicKey, peer.Preshared, peer.AllowedIPs)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open config for append: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(block); err != nil {
		return fmt.Errorf("append peer: %w", err)
	}
	return nil
}

// RemovePeer removes a client peer block from the server config file.
func RemovePeer(path, name string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	marker := "### Client " + name
	lines := strings.Split(string(data), "\n")
	var out []string
	skip := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == marker {
			skip = true
			continue
		}
		if skip {
			if trimmed == "" {
				skip = false
			}
			continue
		}
		out = append(out, line)
	}

	content := strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n"
	return os.WriteFile(path, []byte(content), 0o600)
}

// BuildClientConfig generates a client .conf file content.
func BuildClientConfig(privKey, ipv4, ipv6, dns1, dns2, serverPubKey, psk, endpoint, allowedIPs string) string {
	return fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s/32,%s/128
DNS = %s,%s

[Peer]
PublicKey = %s
PresharedKey = %s
Endpoint = %s
AllowedIPs = %s
`, privKey, ipv4, ipv6, dns1, dns2, serverPubKey, psk, endpoint, allowedIPs)
}
