package library

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"

	"github.com/styxnanda/sailune-go/internal/model"
)

const maxImportBytes = 256 << 20

// Transfer is the portable, credential-free JSON envelope. An absent Format
// identifies the original version-1 JSON storage envelope.
type Transfer struct {
	Format    string     `json:"format,omitempty"`
	Version   int        `json:"version"`
	NextID    int64      `json:"next_id"`
	Bookmarks []Bookmark `json:"bookmarks"`
}

type ImportResult struct {
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
}

func decodeTransfer(r io.Reader) (Transfer, error) {
	var v Transfer
	data, err := io.ReadAll(io.LimitReader(r, maxImportBytes+1))
	if err != nil {
		return v, err
	}
	if len(data) > maxImportBytes {
		return v, errors.New("JSON import exceeds 256 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&v); err != nil {
		return v, fmt.Errorf("invalid library JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return v, errors.New("unexpected trailing JSON data")
	}
	if v.Format != "" && v.Format != "sailune-library" || v.Version != 1 || v.NextID < 1 || v.Bookmarks == nil {
		return v, errors.New("invalid or unsupported library JSON envelope")
	}
	ids, urls := map[int64]bool{}, map[string]bool{}
	for _, b := range v.Bookmarks {
		u, site, work, err := model.NormalizeURL(b.URL)
		if err != nil || u != b.URL || site != b.Site || work != b.WorkID || b.ID < 1 || b.ID >= v.NextID || ids[b.ID] || urls[b.URL] || model.Validate(b) != nil {
			return v, errors.New("invalid or duplicate bookmark in library JSON")
		}
		ids[b.ID], urls[b.URL] = true, true
	}
	return v, nil
}

// Import restores IDs and timestamps into a pristine library. With merge=true,
// canonical URL duplicates are skipped and new records receive local IDs. No
// existing personal data is overwritten, and all inserts commit atomically.
func (l Library) Import(r io.Reader, merge bool) (ImportResult, error) {
	v, err := decodeTransfer(r)
	if err != nil {
		return ImportResult{}, err
	}
	var result ImportResult
	err = l.Store.write(func(tx *sql.Tx) error {
		var next int64
		if err := tx.QueryRow("SELECT next_id FROM library_meta WHERE singleton=1").Scan(&next); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRow("SELECT count(*) FROM bookmarks").Scan(&count); err != nil {
			return err
		}
		if !merge && (count != 0 || next != 1) {
			return errors.New("restore requires a pristine library; choose a new --data path or use --merge (duplicates are skipped)")
		}
		for _, b := range v.Bookmarks {
			if merge {
				var id int64
				err := tx.QueryRow("SELECT id FROM bookmarks WHERE url=?", b.URL).Scan(&id)
				if err == nil {
					result.Skipped++
					continue
				}
				if !errors.Is(err, sql.ErrNoRows) {
					return err
				}
				if next == math.MaxInt64 {
					return errors.New("library ID limit reached")
				}
				b.ID = next
				next++
			}
			if err := saveBookmark(tx, b); err != nil {
				return err
			}
			result.Imported++
		}
		if !merge {
			next = v.NextID
		}
		_, err := tx.Exec("UPDATE library_meta SET next_id=? WHERE singleton=1", next)
		return err
	})
	if err != nil {
		return ImportResult{}, err
	}
	return result, nil
}

func (l Library) ImportFile(path string, merge bool) (ImportResult, error) {
	// Refuse to overwrite/import from the database itself, including hardlink aliases.
	if sameFile(path, l.Store.Path) {
		return ImportResult{}, errors.New("import source must be separate from the SQLite library")
	}
	f, err := os.Open(path)
	if err != nil {
		return ImportResult{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ImportResult{}, err
	}
	if !info.Mode().IsRegular() {
		return ImportResult{}, errors.New("import source must be a regular JSON file")
	}
	return l.Import(f, merge)
}

func sameFile(a, b string) bool {
	aa, _ := filepath.Abs(a)
	bb, _ := filepath.Abs(b)
	if aa == bb {
		return true
	}
	ai, ae := os.Stat(a)
	bi, be := os.Stat(b)
	return ae == nil && be == nil && os.SameFile(ai, bi)
}

// Export streams one consistent SQLite snapshot, preserving IDs and the next-ID
// watermark. It contains no authentication sessions or machine-specific keys.
func (l Library) Export(w io.Writer) error {
	db, err := l.Store.open(false)
	if errors.Is(err, os.ErrNotExist) {
		return json.NewEncoder(w).Encode(Transfer{Format: "sailune-library", Version: 1, NextID: 1, Bookmarks: []Bookmark{}})
	}
	if err != nil {
		return err
	}
	defer db.Close()
	// Explicit deferred transaction: a snapshot reader, not an immediate writer.
	if _, err = db.Exec("BEGIN"); err != nil {
		return err
	}
	defer db.Exec("ROLLBACK")
	var next int64
	if err = db.QueryRow("SELECT next_id FROM library_meta WHERE singleton=1").Scan(&next); err != nil {
		return err
	}
	if _, err = fmt.Fprintf(w, "{\"format\":\"sailune-library\",\"version\":1,\"next_id\":%d,\"bookmarks\":[\n", next); err != nil {
		return err
	}
	rows, err := db.Query("SELECT payload FROM bookmarks ORDER BY id")
	if err != nil {
		return err
	}
	defer rows.Close()
	first := true
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		if !first {
			if _, err = io.WriteString(w, ",\n"); err != nil {
				return err
			}
		}
		first = false
		if _, err = io.WriteString(w, raw); err != nil {
			return err
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	_, err = io.WriteString(w, "\n]}\n")
	return err
}

// ExportFile publishes a complete owner-only file and refuses to overwrite any
// existing destination. Sync this closed snapshot, never the live SQLite file.
func (l Library) ExportFile(path string) error {
	if sameFile(path, l.Store.Path) {
		return errors.New("export destination must be separate from the SQLite library")
	}
	if _, err := os.Lstat(path); err == nil {
		return errors.New("export destination already exists; choose a new filename")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".sailune-export-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := privateFile(f.Name()); err != nil {
		return err
	}
	if err := l.Export(f); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Link(f.Name(), path)
}
