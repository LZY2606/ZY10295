package store

import "fmt"

// migrations are applied in order; each runs exactly once.
var migrations = []string{
	// 1: initial schema
	`CREATE TABLE schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	);
	CREATE TABLE peers (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		addr TEXT NOT NULL
	);
	CREATE TABLE exchanges (
		id INTEGER PRIMARY KEY,
		peer_id INTEGER NOT NULL REFERENCES peers(id),
		seq INTEGER NOT NULL,
		recv_order INTEGER NOT NULL,
		capture_clock TEXT NOT NULL,
		client_known INTEGER NOT NULL,
		t1_unix_ns INTEGER NOT NULL DEFAULT 0,
		t4_unix_ns INTEGER NOT NULL DEFAULT 0,
		t1_sec INTEGER NOT NULL, t1_frac INTEGER NOT NULL,
		t4_sec INTEGER NOT NULL, t4_frac INTEGER NOT NULL,
		li INTEGER NOT NULL, vn INTEGER NOT NULL, mode INTEGER NOT NULL,
		stratum INTEGER NOT NULL, poll INTEGER NOT NULL, precision INTEGER NOT NULL,
		root_delay_sec INTEGER NOT NULL, root_delay_frac INTEGER NOT NULL,
		root_disp_sec INTEGER NOT NULL, root_disp_frac INTEGER NOT NULL,
		refid TEXT NOT NULL,
		ref_sec INTEGER NOT NULL, ref_frac INTEGER NOT NULL,
		orig_sec INTEGER NOT NULL, orig_frac INTEGER NOT NULL,
		rx_sec INTEGER NOT NULL, rx_frac INTEGER NOT NULL,
		tx_sec INTEGER NOT NULL, tx_frac INTEGER NOT NULL,
		raw BLOB NOT NULL
	);
	CREATE INDEX idx_exchanges_peer ON exchanges(peer_id, recv_order);
	CREATE TABLE anchors (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		unix_sec INTEGER NOT NULL,
		note TEXT NOT NULL DEFAULT ''
	);`,
}

func (s *Store) migrate() error {
	var version int
	row := s.db.QueryRow(`SELECT COUNT(name) FROM sqlite_master WHERE type='table' AND name='schema_migrations'`)
	var haveTable int
	if err := row.Scan(&haveTable); err != nil {
		return err
	}
	if haveTable > 0 {
		if err := s.db.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&version); err != nil {
			return err
		}
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version) VALUES(?)`, i+1); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
