package wgconf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseContent(t *testing.T) {
	content := `[Interface]
Address = 10.66.66.1/24
PrivateKey = serverprivkey
ListenPort = 51820

### Client alice
[Peer]
PublicKey = alicepub
PresharedKey = alicepsk
AllowedIPs = 10.66.66.2/32,fd42:42:42::2/128

### Client bob
[Peer]
PublicKey = bobpub
PresharedKey = bobpsk
AllowedIPs = 10.66.66.3/32,fd42:42:42::3/128
`
	peers, err := ParseContent(content)
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(peers))
	}
	if peers[0].Name != "alice" || peers[0].IPv4 != "10.66.66.2" {
		t.Fatalf("unexpected alice: %+v", peers[0])
	}
}

func TestFindFreeIP(t *testing.T) {
	used := []string{"10.66.66.2", "10.66.66.3"}
	ip, err := FindFreeIP("10.66.66.1", used)
	if err != nil {
		t.Fatal(err)
	}
	if ip != 4 {
		t.Fatalf("expected 4, got %d", ip)
	}
}

func TestRemovePeer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wg0.conf")
	initial := ` [Interface]
PrivateKey = x

### Client test
[Peer]
PublicKey = y
PresharedKey = z
AllowedIPs = 10.0.0.2/32

`
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RemovePeer(path, "test"); err != nil {
		t.Fatal(err)
	}
	peers, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 0 {
		t.Fatalf("expected 0 peers after remove, got %d", len(peers))
	}
}

func TestBuildClientConfig(t *testing.T) {
	cfg := BuildClientConfig("priv", "10.0.0.2", "fd42::2", "1.1.1.1", "1.0.0.1", "spub", "psk", "1.2.3.4:51820", "0.0.0.0/0")
	if cfg == "" {
		t.Fatal("empty config")
	}
}
