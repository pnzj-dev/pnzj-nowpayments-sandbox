package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Store persists entities as JSON blobs in SQLite and keeps an in-memory index.
type Store struct {
	db  *sql.DB
	mu  sync.RWMutex
	mem map[string]map[string]json.RawMessage // kind -> id -> json
	seq map[string]int64                      // kind -> last id
}

func NewStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS entities (
		kind TEXT NOT NULL,
		id TEXT NOT NULL,
		json TEXT NOT NULL,
		updated_at INTEGER NOT NULL,
		PRIMARY KEY (kind, id)
	)`); err != nil {
		return nil, err
	}
	s := &Store{db: db, mem: map[string]map[string]json.RawMessage{}, seq: map[string]int64{}}
	// load existing into memory
	rows, err := db.Query(`SELECT kind, id, json FROM entities`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, id string
		var raw []byte
		if err := rows.Scan(&kind, &id, &raw); err != nil {
			return nil, err
		}
		if s.mem[kind] == nil {
			s.mem[kind] = map[string]json.RawMessage{}
		}
		s.mem[kind][id] = raw
		var n int64
		fmt.Sscanf(id, "%d", &n)
		if n > s.seq[kind] {
			s.seq[kind] = n
		}
	}
	return s, nil
}

func (s *Store) NextID(kind string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq[kind]++
	return fmt.Sprintf("%d", s.seq[kind])
}

func (s *Store) Put(kind, id string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.mem[kind] == nil {
		s.mem[kind] = map[string]json.RawMessage{}
	}
	s.mem[kind][id] = raw
	s.mu.Unlock()
	_, err = s.db.Exec(`INSERT OR REPLACE INTO entities (kind, id, json, updated_at) VALUES (?, ?, ?, ?)`,
		kind, id, string(raw), time.Now().Unix())
	return err
}

func (s *Store) Get(kind, id string, out any) bool {
	s.mu.RLock()
	raw, ok := s.mem[kind][id]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	return json.Unmarshal(raw, out) == nil
}

func (s *Store) List(kind string) []json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]json.RawMessage, 0, len(s.mem[kind]))
	for _, raw := range s.mem[kind] {
		out = append(out, raw)
	}
	return out
}

func (s *Store) Close() error { return s.db.Close() }
