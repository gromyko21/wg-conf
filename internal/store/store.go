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
	Name              string
	PublicKey         string
	IPv4              string
	IPv6              string
	Enabled           bool
	CreatedAt         time.Time
	CreatedBy         string
	ClientConfig      string
	MonthlyLimitBytes int64
}

type MonthlyTraffic struct {
	PeerName       string
	YearMonth      string
	UploadBytes    int64
	DownloadBytes  int64
	TotalBytes     int64
	LimitBytes     int64
	LimitExceeded  bool
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

CREATE TABLE IF NOT EXISTS monthly_traffic (
    peer_name TEXT NOT NULL,
    year_month TEXT NOT NULL,
    upload_bytes INTEGER NOT NULL DEFAULT 0,
    download_bytes INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (peer_name, year_month)
);

CREATE TABLE IF NOT EXISTS traffic_baselines (
    peer_name TEXT NOT NULL,
    year_month TEXT NOT NULL,
    rx_bytes INTEGER NOT NULL DEFAULT 0,
    tx_bytes INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (peer_name, year_month)
);
`
	_, err := s.db.Exec(schema)
	if err != nil {
		return err
	}
	_, _ = s.db.Exec(`ALTER TABLE peers ADD COLUMN monthly_limit_bytes INTEGER NOT NULL DEFAULT 0`)
	return nil
}

func (s *Store) UpsertPeer(ctx context.Context, p PeerRecord) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO peers (name, public_key, ipv4, ipv6, enabled, created_at, created_by, client_config, monthly_limit_bytes)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
    public_key=excluded.public_key,
    ipv4=excluded.ipv4,
    ipv6=excluded.ipv6,
    enabled=excluded.enabled,
    client_config=CASE WHEN excluded.client_config != '' THEN excluded.client_config ELSE peers.client_config END,
    monthly_limit_bytes=CASE WHEN excluded.monthly_limit_bytes > 0 THEN excluded.monthly_limit_bytes ELSE peers.monthly_limit_bytes END
`, p.Name, p.PublicKey, p.IPv4, p.IPv6, boolToInt(p.Enabled), p.CreatedAt.UTC().Format(time.RFC3339), p.CreatedBy, p.ClientConfig, p.MonthlyLimitBytes)
	return err
}

func (s *Store) SetPeerLimit(ctx context.Context, name string, limitBytes int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE peers SET monthly_limit_bytes = ? WHERE name = ?`, limitBytes, name)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) SetPeerEnabled(ctx context.Context, name string, enabled bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE peers SET enabled = ? WHERE name = ?`, boolToInt(enabled), name)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DeletePeer(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM peers WHERE name = ?`, name)
	return err
}

func (s *Store) ListPeers(ctx context.Context) ([]PeerRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT name, public_key, ipv4, ipv6, enabled, created_at, created_by, client_config, monthly_limit_bytes FROM peers ORDER BY name
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
		if err := rows.Scan(&p.Name, &p.PublicKey, &p.IPv4, &p.IPv6, &enabled, &createdAt, &p.CreatedBy, &p.ClientConfig, &p.MonthlyLimitBytes); err != nil {
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
SELECT name, public_key, ipv4, ipv6, enabled, created_at, created_by, client_config, monthly_limit_bytes FROM peers WHERE name = ?
`, name).Scan(&p.Name, &p.PublicKey, &p.IPv4, &p.IPv6, &enabled, &createdAt, &p.CreatedBy, &p.ClientConfig, &p.MonthlyLimitBytes)
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

func (s *Store) SetMonthlyTraffic(ctx context.Context, peerName, yearMonth string, uploadBytes, downloadBytes int64) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO monthly_traffic (peer_name, year_month, upload_bytes, download_bytes)
VALUES (?, ?, ?, ?)
ON CONFLICT(peer_name, year_month) DO UPDATE SET
    upload_bytes = excluded.upload_bytes,
    download_bytes = excluded.download_bytes
`, peerName, yearMonth, uploadBytes, downloadBytes)
	return err
}

func (s *Store) GetTrafficBaseline(ctx context.Context, peerName, yearMonth string) (rx, tx int64, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `
SELECT rx_bytes, tx_bytes FROM traffic_baselines WHERE peer_name = ? AND year_month = ?
`, peerName, yearMonth).Scan(&rx, &tx)
	if err == sql.ErrNoRows {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, err
	}
	return rx, tx, true, nil
}

func (s *Store) SetTrafficBaseline(ctx context.Context, peerName, yearMonth string, rx, tx int64) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO traffic_baselines (peer_name, year_month, rx_bytes, tx_bytes)
VALUES (?, ?, ?, ?)
ON CONFLICT(peer_name, year_month) DO NOTHING
`, peerName, yearMonth, rx, tx)
	return err
}

func (s *Store) HasTrafficInMonth(ctx context.Context, peerName, yearMonth string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(1) FROM monthly_traffic WHERE peer_name = ? AND year_month = ?
`, peerName, yearMonth).Scan(&n)
	return n > 0, err
}

func (s *Store) EarliestSnapshotInMonth(ctx context.Context, peerName string, monthStart, monthEnd time.Time) (rx, tx int64, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `
SELECT rx_bytes, tx_bytes FROM usage_snapshots
WHERE peer_name = ? AND collected_at >= ? AND collected_at < ?
ORDER BY collected_at ASC LIMIT 1
`, peerName, monthStart.UTC().Format(time.RFC3339), monthEnd.UTC().Format(time.RFC3339)).Scan(&rx, &tx)
	if err == sql.ErrNoRows {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, err
	}
	return rx, tx, true, nil
}

func (s *Store) ResetMonthTraffic(ctx context.Context, yearMonth string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM monthly_traffic WHERE year_month = ?`, yearMonth); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM traffic_baselines WHERE year_month = ?`, yearMonth)
	return err
}

func (s *Store) MonthlyTrafficByPeer(ctx context.Context, yearMonth string) (map[string]MonthlyTraffic, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT m.peer_name, m.year_month, m.upload_bytes, m.download_bytes, COALESCE(p.monthly_limit_bytes, 0)
FROM monthly_traffic m
LEFT JOIN peers p ON p.name = m.peer_name
WHERE m.year_month = ?
`, yearMonth)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]MonthlyTraffic)
	for rows.Next() {
		var m MonthlyTraffic
		if err := rows.Scan(&m.PeerName, &m.YearMonth, &m.UploadBytes, &m.DownloadBytes, &m.LimitBytes); err != nil {
			return nil, err
		}
		m.TotalBytes = m.UploadBytes + m.DownloadBytes
		if m.LimitBytes > 0 && m.TotalBytes >= m.LimitBytes {
			m.LimitExceeded = true
		}
		result[m.PeerName] = m
	}
	return result, rows.Err()
}

func (s *Store) MonthlyTrafficTotals(ctx context.Context, yearMonth string) (upload, download int64, err error) {
	err = s.db.QueryRowContext(ctx, `
SELECT COALESCE(SUM(upload_bytes), 0), COALESCE(SUM(download_bytes), 0)
FROM monthly_traffic WHERE year_month = ?
`, yearMonth).Scan(&upload, &download)
	return upload, download, err
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
