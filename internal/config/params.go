package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ServerParams holds settings from /etc/wireguard/params (angristan format).
type ServerParams struct {
	ServerPubIP   string
	ServerPubNIC  string
	ServerWGNIC   string
	ServerWGIPv4  string
	ServerWGIPv6  string
	ServerPort    string
	ServerPrivKey string
	ServerPubKey  string
	ClientDNS1    string
	ClientDNS2    string
	AllowedIPs    string
}

func LoadParams(path string) (*ServerParams, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open params: %w", err)
	}
	defer f.Close()

	vals := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		vals[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read params: %w", err)
	}

	p := &ServerParams{
		ServerPubIP:   vals["SERVER_PUB_IP"],
		ServerPubNIC:  vals["SERVER_PUB_NIC"],
		ServerWGNIC:   vals["SERVER_WG_NIC"],
		ServerWGIPv4:  vals["SERVER_WG_IPV4"],
		ServerWGIPv6:  vals["SERVER_WG_IPV6"],
		ServerPort:    vals["SERVER_PORT"],
		ServerPrivKey: vals["SERVER_PRIV_KEY"],
		ServerPubKey:  vals["SERVER_PUB_KEY"],
		ClientDNS1:    vals["CLIENT_DNS_1"],
		ClientDNS2:    vals["CLIENT_DNS_2"],
		AllowedIPs:    vals["ALLOWED_IPS"],
	}

	if p.ServerWGNIC == "" || p.ServerWGIPv4 == "" {
		return nil, fmt.Errorf("invalid params file: missing required fields")
	}
	if p.AllowedIPs == "" {
		p.AllowedIPs = "0.0.0.0/0,::/0"
	}
	if p.ClientDNS2 == "" {
		p.ClientDNS2 = p.ClientDNS1
	}

	return p, nil
}

func (p *ServerParams) WGConfPath(dir string) string {
	return filepath.Join(dir, p.ServerWGNIC+".conf")
}

func (p *ServerParams) Endpoint() string {
	ip := p.ServerPubIP
	if strings.Contains(ip, ":") {
		if !strings.HasPrefix(ip, "[") {
			ip = "[" + ip + "]"
		}
	}
	return ip + ":" + p.ServerPort
}
