// Package store persists imported NTP exchanges, peers and trusted
// anchors in SQLite. Schema changes are applied as ordered migrations.
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"

	"ntp-ledger/internal/ledger"
	"ntp-ledger/internal/ntp"
)

// Store wraps the SQLite handle.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the database and applies migrations.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`); err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Anchor is a trusted time anchor used for era selection.
type Anchor struct {
	ID      int64
	Name    string
	UnixSec int64
	Note    string
}

// Abs converts the anchor to an absolute NTP time (fraction zero).
func (a Anchor) Abs() ntp.Abs {
	ntpSec := a.UnixSec + ntp.UnixEpochOffset
	return ntp.Abs{Era: uint32(uint64(ntpSec) / ntp.EraSpan), Sec: uint32(uint64(ntpSec) % ntp.EraSpan)}
}

// IsEmpty reports whether any peer has been imported.
func (s *Store) IsEmpty() (bool, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM peers`).Scan(&n); err != nil {
		return false, err
	}
	return n == 0, nil
}

// InsertPeer inserts a peer and returns its id.
func (s *Store) InsertPeer(name, addr string) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO peers(name, addr) VALUES(?, ?)`, name, addr)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// InsertExchange persists one exchange with its raw packet.
func (s *Store) InsertExchange(ex *ledger.Exchange) (int64, error) {
	p := ex.Resp
	res, err := s.db.Exec(`INSERT INTO exchanges(
		peer_id, seq, recv_order, capture_clock, client_known, t1_unix_ns, t4_unix_ns,
		t1_sec, t1_frac, t4_sec, t4_frac,
		li, vn, mode, stratum, poll, precision,
		root_delay_sec, root_delay_frac, root_disp_sec, root_disp_frac, refid,
		ref_sec, ref_frac, orig_sec, orig_frac, rx_sec, rx_frac, tx_sec, tx_frac, raw
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		ex.PeerID, ex.Seq, ex.RecvOrder, ex.CaptureClock, ex.ClientKnown,
		ex.T1UnixNs, ex.T4UnixNs,
		ex.T1.Sec, ex.T1.Frac, ex.T4.Sec, ex.T4.Frac,
		p.LI, p.VN, p.Mode, p.Stratum, p.Poll, p.Precision,
		p.RootDelay.Sec, p.RootDelay.Frac, p.RootDisp.Sec, p.RootDisp.Frac, string(p.RefID[:]),
		p.Ref.Sec, p.Ref.Frac, p.Orig.Sec, p.Orig.Frac,
		p.Rx.Sec, p.Rx.Frac, p.Tx.Sec, p.Tx.Frac, ex.Raw)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// LoadPeers reads all peers and their exchanges back, ordered by id.
func (s *Store) LoadPeers() ([]ledger.PeerInput, error) {
	rows, err := s.db.Query(`SELECT id, name, addr FROM peers ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var peers []ledger.PeerInput
	idx := map[int64]int{}
	for rows.Next() {
		var p ledger.PeerInput
		if err := rows.Scan(&p.ID, &p.Name, &p.Addr); err != nil {
			return nil, err
		}
		idx[p.ID] = len(peers)
		peers = append(peers, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	exRows, err := s.db.Query(`SELECT
		id, peer_id, seq, recv_order, capture_clock, client_known, t1_unix_ns, t4_unix_ns,
		t1_sec, t1_frac, t4_sec, t4_frac,
		li, vn, mode, stratum, poll, precision,
		root_delay_sec, root_delay_frac, root_disp_sec, root_disp_frac, refid,
		ref_sec, ref_frac, orig_sec, orig_frac, rx_sec, rx_frac, tx_sec, tx_frac, raw
		FROM exchanges ORDER BY peer_id, recv_order`)
	if err != nil {
		return nil, err
	}
	defer exRows.Close()
	for exRows.Next() {
		var ex ledger.Exchange
		var refid string
		var raw []byte
		err := exRows.Scan(
			&ex.ID, &ex.PeerID, &ex.Seq, &ex.RecvOrder, &ex.CaptureClock, &ex.ClientKnown,
			&ex.T1UnixNs, &ex.T4UnixNs,
			&ex.T1.Sec, &ex.T1.Frac, &ex.T4.Sec, &ex.T4.Frac,
			&ex.Resp.LI, &ex.Resp.VN, &ex.Resp.Mode, &ex.Resp.Stratum,
			&ex.Resp.Poll, &ex.Resp.Precision,
			&ex.Resp.RootDelay.Sec, &ex.Resp.RootDelay.Frac,
			&ex.Resp.RootDisp.Sec, &ex.Resp.RootDisp.Frac, &refid,
			&ex.Resp.Ref.Sec, &ex.Resp.Ref.Frac,
			&ex.Resp.Orig.Sec, &ex.Resp.Orig.Frac,
			&ex.Resp.Rx.Sec, &ex.Resp.Rx.Frac,
			&ex.Resp.Tx.Sec, &ex.Resp.Tx.Frac, &raw)
		if err != nil {
			return nil, err
		}
		copy(ex.Resp.RefID[:], refid)
		ex.Raw = raw
		i, ok := idx[ex.PeerID]
		if !ok {
			return nil, fmt.Errorf("exchange %d references unknown peer %d", ex.ID, ex.PeerID)
		}
		e := ex
		peers[i].Exchanges = append(peers[i].Exchanges, &e)
	}
	return peers, exRows.Err()
}

// AddAnchor stores a trusted anchor.
func (s *Store) AddAnchor(name string, unixSec int64, note string) error {
	_, err := s.db.Exec(`INSERT INTO anchors(name, unix_sec, note) VALUES(?,?,?)`, name, unixSec, note)
	return err
}

// Anchors lists all anchors by id.
func (s *Store) Anchors() ([]Anchor, error) {
	rows, err := s.db.Query(`SELECT id, name, unix_sec, note FROM anchors ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Anchor
	for rows.Next() {
		var a Anchor
		if err := rows.Scan(&a.ID, &a.Name, &a.UnixSec, &a.Note); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
