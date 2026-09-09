package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/styxnanda/sailune-go/internal/model"
)

// Store serializes mutations across processes and replaces the data atomically.
// Keep the file on a local filesystem. Readers see either complete version.
type Store struct{ Path string }

type database struct {
	Version   int        `json:"version"`
	NextID    int64      `json:"next_id"`
	Bookmarks []Bookmark `json:"bookmarks"`
}

func (s Store) read() (database, error) {
	db := database{Version: 1, NextID: 1, Bookmarks: []Bookmark{}}
	if s.Path == "" {
		return db, errors.New("library path is empty")
	}
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return db, nil
	}
	if err != nil {
		return db, err
	}
	// Decode into an empty value so missing required fields cannot be accepted.
	var saved database
	if err := json.Unmarshal(data, &saved); err != nil {
		return db, fmt.Errorf("read library (file left untouched): %w", err)
	}
	if saved.Version != 1 || saved.NextID < 1 || saved.Bookmarks == nil {
		return db, errors.New("invalid or unsupported library format (file left untouched)")
	}
	ids, urls := map[int64]bool{}, map[string]bool{}
	for _, b := range saved.Bookmarks {
		u, site, work, err := model.NormalizeURL(b.URL)
		if err != nil || u != b.URL || site != b.Site || work != b.WorkID || b.ID < 1 || b.ID >= saved.NextID || ids[b.ID] || urls[b.URL] || model.Validate(b) != nil {
			return db, errors.New("invalid library record (file left untouched)")
		}
		ids[b.ID], urls[b.URL] = true, true
	}
	return saved, nil
}

func (s Store) change(fn func(*database) error) error {
	if s.Path == "" {
		return errors.New("library path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return err
	}
	lock := s.Path + ".lock"
	f, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("library is locked: %s (if a previous process crashed, remove this lock only after verifying no writer is running)", lock)
	}
	if err != nil {
		return err
	}
	f.Close()
	defer os.Remove(lock)
	db, err := s.read()
	if err != nil {
		return err
	}
	if err := fn(&db); err != nil {
		return err
	}
	data, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".sailune-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.Path); err != nil {
		return err
	}
	return nil
}
