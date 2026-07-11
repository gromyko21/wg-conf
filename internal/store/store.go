package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type PeerRecord struct {
	Name         string
	PublicKey    string
	IPv4         string
	IPv6         string
	Enabled      bool
	CreatedAt    time.Time
	CreatedBy    string
	ClientConfig string
}

type UsageSnapshot struct {
	PeerName      string
	PublicKey     string
	RxBytes       int64
	TxBytes       int64
	LastHandshake time.Time
	Online        bool
	Endpoint      string
	CollectedAt   time.Time
}

type AuditEntry struct {
	ID        int64
	Actor     string
	Action    string
	PeerName  string
	Details   string
	CreatedAt time.Time
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	schema := `
CREATE TABLE IF NOT EXISTS peers (
    name TEXT PRIMARY KEY,
    public_key TEXT NOT NULL UNIQUE,
    ipv4 TEXT NOT NULL,
    ipv6 TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    created_by TEXT NOT NULL DEFAULT '',
    client_config TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS usage_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    peer_name TEXT NOT NULL,
    public_key TEXT NOT NULL,
    rx_bytes INTEGER NOT NULL,
    tx_bytes INTEGER NOT NULL,
    last_handshake TEXT,
    online INTEGER NOT NULL,
    endpoint TEXT,
    collected_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_usage_peer_time ON usage_snapshots(peer_name, collected_at);

CREATE TABLE IF NOT EXISTS audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    actor TEXT NOT NULL,
    action TEXT NOT NULL,
    peer_name TEXT,
    details TEXT,
    created_at TEXT NOT NULL
);
`
	_, err := s.db.Exec(schema)
	return err
}

func (s *Store) UpsertPeer(ctx context.Context, p PeerRecord) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO peers (name, public_key, ipv4, ipv6, enabled, created_at, created_by, client_config)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
    public_key=excluded.public_key,
    ipv4=excluded.ipv4,
    ipv6=excluded.ipv6,
    enabled=excluded.enabled,
    client_config=CASE WHEN excluded.client_config != '' THEN excluded.client_config ELSE peers.client_config END
`, p.Name, p.PublicKey, p.IPv4, p.IPv6, boolToInt(p.Enabled), p.CreatedAt.UTC().Format(time.RFC3339), p.CreatedBy, p.ClientConfig)
	return err
}

func (s *Store) DeletePeer(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM peers WHERE name = ?`, name)
	return err
}

func (s *Store) ListPeers(ctx context.Context) ([]PeerRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT name, public_key, ipv4, ipv6, enabled, created_at, created_by, client_config FROM peers ORDER BY name
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var peers []PeerRecord
	for rows.Next() {
		var p PeerRecord
		var enabled int
		var createdAt string
		if err := rows.Scan(&p.Name, &p.PublicKey, &p.IPv4, &p.IPv6, &enabled, &createdAt, &p.CreatedBy, &p.ClientConfig); err != nil {
			return nil, err
		}
		p.Enabled = enabled == 1
		p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		peers = append(peers, p)
	}
	return peers, rows.Err()
}

func (s *Store) GetPeer(ctx context.Context, name string) (*PeerRecord, error) {
	var p PeerRecord
	var enabled int
	var createdAt string
	err := s.db.QueryRowContext(ctx, `
SELECT name, public_key, ipv4, ipv6, enabled, created_at, created_by, client_config FROM peers WHERE name = ?
`, name).Scan(&p.Name, &p.PublicKey, &p.IPv4, &p.IPv6, &enabled, &createdAt, &p.CreatedBy, &p.ClientConfig)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.Enabled = enabled == 1
	p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &p, nil
}

func (s *Store) SaveUsageSnapshot(ctx context.Context, snap UsageSnapshot) error {
	var hs string
	if !snap.LastHandshake.IsZero() {
		hs = snap.LastHandshake.UTC().Format(time.RFC3339)
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO usage_snapshots (peer_name, public_key, rx_bytes, tx_bytes, last_handshake, online, endpoint, collected_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
`, snap.PeerName, snap.PublicKey, snap.RxBytes, snap.TxBytes, hs, boolToInt(snap.Online), snap.Endpoint, snap.CollectedAt.UTC().Format(time.RFC3339))
	return err
}

func (s *Store) LatestUsageByPeer(ctx context.Context) (map[string]UsageSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT peer_name, public_key, rx_bytes, tx_bytes, last_handshake, online, endpoint, collected_at
FROM usage_snapshots
WHERE id IN (SELECT MAX(id) FROM usage_snapshots GROUP BY peer_name)
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]UsageSnapshot)
	for rows.Next() {
		var snap UsageSnapshot
		var hs sql.NullString
		var online int
		var collectedAt string
		if err := rows.Scan(&snap.PeerName, &snap.PublicKey, &snap.RxBytes, &snap.TxBytes, &hs, &online, &snap.Endpoint, &collectedAt); err != nil {
			return nil, err
		}
		if hs.Valid {
			snap.LastHandshake, _ = time.Parse(time.RFC3339, hs.String)
		}
		snap.Online = online == 1
		snap.CollectedAt, _ = time.Parse(time.RFC3339, collectedAt)
		result[snap.PeerName] = snap
	}
	return result, rows.Err()
}

func (s *Store) AddAudit(ctx context.Context, actor, action, peerName, details string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO audit_log (actor, action, peer_name, details, created_at) VALUES (?, ?, ?, ?, ?)
`, actor, action, peerName, details, time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *Store) ListAudit(ctx context.Context, limit int) ([]AuditEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, actor, action, peer_name, details, created_at FROM audit_log ORDER BY id DESC LIMIT ?
`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var peerName, details sql.NullString
		var createdAt string
		if err := rows.Scan(&e.ID, &e.Actor, &e.Action, &peerName, &details, &createdAt); err != nil {
			return nil, err
		}
		if peerName.Valid {
			e.PeerName = peerName.String
		}
		if details.Valid {
			e.Details = details.String
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (s *Store) SyncPeersFromConfig(ctx context.Context, peers []PeerRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, p := range peers {
		_, err := tx.ExecContext(ctx, `
INSERT INTO peers (name, public_key, ipv4, ipv6, enabled, created_at, created_by, client_config)
VALUES (?, ?, ?, ?, 1, ?, 'import', ?)
ON CONFLICT(name) DO UPDATE SET
	public_key=excluded.public_key,
	ipv4=excluded.ipv4,
	ipv6=excluded.ipv6,
	client_config=CASE WHEN excluded.client_config != '' THEN excluded.client_config ELSE peers.client_config END
`, p.Name, p.PublicKey, p.IPv4, p.IPv6, p.CreatedAt.UTC().Format(time.RFC3339), p.ClientConfig)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Store) PrunePeersExcept(ctx context.Context, keep []string) error {
	if len(keep) == 0 {
		_, err := s.db.ExecContext(ctx, `DELETE FROM peers`)
		return err
	}

	placeholders := make([]string, len(keep))
	args := make([]any, len(keep))
	for i, name := range keep {
		placeholders[i] = "?"
		args[i] = name
	}
	query := fmt.Sprintf(`DELETE FROM peers WHERE name NOT IN (%s)`, strings.Join(placeholders, ","))
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
