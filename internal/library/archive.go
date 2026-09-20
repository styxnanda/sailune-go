package library

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/styxnanda/sailune-go/internal/model"
)

const MaxArchiveBytes int64 = 512 << 20
const maxArchiveRecords = 64 << 20

type archiveManifest struct {
	Format  string `json:"format"`
	Version int    `json:"version"`
}
type membership struct {
	Collection string `json:"collection"`
	Story      int64  `json:"story"`
}
type archiveLibrary struct {
	NextID      int64        `json:"next_id"`
	Bookmarks   []Bookmark   `json:"bookmarks"`
	Collections []Collection `json:"collections"`
	Members     []membership `json:"members"`
	Artwork     []Artwork    `json:"artwork"`
}

func writeJSON(z *zip.Writer, name string, v any) error {
	w, err := z.Create(name)
	if err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(v)
}

// boundedArchiveWriter streams record JSON while enforcing import-compatible limits.
type boundedArchiveWriter struct {
	writer io.Writer
	bytes  int64
}

func (w *boundedArchiveWriter) Write(p []byte) (int, error) {
	if w.bytes+int64(len(p)) > maxArchiveRecords {
		return 0, errors.New("backup records exceed 64 MiB")
	}
	n, e := w.writer.Write(p)
	w.bytes += int64(n)
	return n, e
}
func (l Library) ExportArchive(w io.Writer) error {
	db, err := l.Store.open(true)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err = db.Exec("BEGIN"); err != nil {
		return err
	}
	defer db.Exec("ROLLBACK")
	var next int64
	if err = db.QueryRow("SELECT next_id FROM library_meta").Scan(&next); err != nil {
		return err
	}
	z := zip.NewWriter(w)
	if err = writeJSON(z, "manifest.json", archiveManifest{"sailune-library", 2}); err != nil {
		return err
	}
	entry, err := z.Create("library.json")
	if err != nil {
		return err
	}
	records := &boundedArchiveWriter{writer: entry}
	if _, err = fmt.Fprintf(records, "{\"next_id\":%d", next); err != nil {
		return err
	}
	arrays := []struct {
		name, query string
		scan        func(*sql.Rows) ([]byte, error)
	}{
		{"bookmarks", "SELECT payload FROM bookmarks ORDER BY id", func(rows *sql.Rows) ([]byte, error) { var raw string; e := rows.Scan(&raw); return []byte(raw), e }},
		{"collections", "SELECT id,name,kind,rules FROM collections ORDER BY id", func(rows *sql.Rows) ([]byte, error) {
			var c Collection
			var raw string
			if e := rows.Scan(&c.ID, &c.Name, &c.Kind, &raw); e != nil {
				return nil, e
			}
			if e := json.Unmarshal([]byte(raw), &c.Rules); e != nil {
				return nil, e
			}
			return json.Marshal(c)
		}},
		{"members", "SELECT collection_id,bookmark_id FROM collection_members ORDER BY collection_id,bookmark_id", func(rows *sql.Rows) ([]byte, error) {
			var m membership
			if e := rows.Scan(&m.Collection, &m.Story); e != nil {
				return nil, e
			}
			return json.Marshal(m)
		}},
		{"artwork", "SELECT bookmark_id,role,asset_id,x,y,width,height FROM story_artwork JOIN artwork_assets ON asset_id=artwork_assets.id ORDER BY bookmark_id,role", func(rows *sql.Rows) ([]byte, error) {
			var a Artwork
			if e := rows.Scan(&a.StoryID, &a.Role, &a.AssetID, &a.X, &a.Y, &a.Width, &a.Height); e != nil {
				return nil, e
			}
			return json.Marshal(a)
		}},
	}
	for _, array := range arrays {
		if _, err = fmt.Fprintf(records, ",\"%s\":[", array.name); err != nil {
			return err
		}
		rows, e := db.Query(array.query)
		if e != nil {
			return e
		}
		first := true
		for rows.Next() {
			raw, e := array.scan(rows)
			if e != nil {
				rows.Close()
				return e
			}
			if !first {
				if _, e = records.Write([]byte(",")); e != nil {
					rows.Close()
					return e
				}
			}
			first = false
			if _, e = records.Write(raw); e != nil {
				rows.Close()
				return e
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if _, e = records.Write([]byte("]")); e != nil {
			return e
		}
	}
	if _, err = records.Write([]byte("}")); err != nil {
		return err
	}
	rows, err := db.Query("SELECT id,data FROM artwork_assets WHERE id IN (SELECT asset_id FROM story_artwork) ORDER BY id")
	if err != nil {
		return err
	}
	defer rows.Close()
	total := records.bytes
	count := 0
	for rows.Next() {
		var id string
		var data []byte
		if err = rows.Scan(&id, &data); err != nil {
			return err
		}
		count++
		total += int64(len(data))
		if count > 10000 || total > MaxArchiveBytes-maxArchiveRecords {
			return errors.New("backup exceeds 10000 images or 448 MiB expanded content")
		}
		entry, err = z.CreateHeader(&zip.FileHeader{Name: "assets/" + id + ".jpg", Method: zip.Store})
		if err != nil {
			return err
		}
		if _, err = entry.Write(data); err != nil {
			return err
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	return z.Close()
}

func (l Library) ExportArchiveFile(path string) error {
	if sameFile(path, l.Store.Path) {
		return errors.New("backup must be separate from library")
	}
	if _, err := os.Lstat(path); err == nil {
		return errors.New("export destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".sailune-backup-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = privateFile(f.Name()); err != nil {
		return err
	}
	if err = l.ExportArchive(f); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return publishDatabase(f.Name(), path)
}
func strictJSON(data []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}
func zipBytes(f *zip.File, limit int64) ([]byte, error) {
	if f.UncompressedSize64 > uint64(limit) {
		return nil, errors.New("archive entry exceeds size limit")
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if int64(len(b)) > limit {
		return nil, errors.New("archive entry exceeds size limit")
	}
	return b, err
}
func (l Library) ImportBackupFile(path string, merge bool) (ImportResult, error) {
	if sameFile(path, l.Store.Path) {
		return ImportResult{}, errors.New("backup must be separate from library")
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
	if !info.Mode().IsRegular() || info.Size() > MaxArchiveBytes {
		return ImportResult{}, errors.New("backup must be a regular file of at most 512 MiB")
	}
	var magic [4]byte
	n, _ := f.Read(magic[:])
	if n != 4 || string(magic[:2]) != "PK" {
		if _, err = f.Seek(0, 0); err != nil {
			return ImportResult{}, err
		}
		return l.Import(f, merge)
	}
	z, err := zip.NewReader(f, info.Size())
	if err != nil {
		return ImportResult{}, err
	}
	return l.importArchive(z, merge)
}
func (l Library) importArchive(z *zip.Reader, merge bool) (ImportResult, error) {
	var result ImportResult
	if len(z.File) > 10002 {
		return result, errors.New("archive has too many entries")
	}
	entries := map[string]*zip.File{}
	var total uint64
	for _, f := range z.File {
		if entries[f.Name] != nil {
			return result, errors.New("duplicate archive entry")
		}
		if f.Name != "manifest.json" && f.Name != "library.json" {
			name := strings.TrimSuffix(strings.TrimPrefix(f.Name, "assets/"), ".jpg")
			if len(name) != 64 || f.Name != "assets/"+name+".jpg" || strings.Trim(name, "0123456789abcdef") != "" {
				return result, errors.New("invalid archive path")
			}
		}
		if f.Mode()&os.ModeSymlink != 0 {
			return result, errors.New("archive symlinks are unsupported")
		}
		if f.UncompressedSize64 > uint64(MaxArchiveBytes) || total > uint64(MaxArchiveBytes)-f.UncompressedSize64 {
			return result, errors.New("expanded backup exceeds 512 MiB")
		}
		total += f.UncompressedSize64
		entries[f.Name] = f
	}
	if entries["manifest.json"] == nil || entries["library.json"] == nil {
		return result, errors.New("missing backup manifest or library")
	}
	raw, err := zipBytes(entries["manifest.json"], 4096)
	if err != nil {
		return result, err
	}
	var m archiveManifest
	if err = strictJSON(raw, &m); err != nil {
		return result, err
	}
	if m.Format != "sailune-library" || m.Version != 2 {
		return result, errors.New("unsupported backup format")
	}
	raw, err = zipBytes(entries["library.json"], maxArchiveRecords)
	if err != nil {
		return result, err
	}
	var a archiveLibrary
	if err = strictJSON(raw, &a); err != nil {
		return result, err
	}
	if a.NextID < 1 || a.Bookmarks == nil {
		return result, errors.New("invalid library records")
	}
	stories := map[int64]bool{}
	urls := map[string]bool{}
	for _, b := range a.Bookmarks {
		u, s, w, e := model.NormalizeURL(b.URL)
		if e != nil || u != b.URL || s != b.Site || w != b.WorkID || b.ID < 1 || b.ID >= a.NextID || stories[b.ID] || urls[b.URL] || model.Validate(b) != nil {
			return result, errors.New("invalid or duplicate story")
		}
		stories[b.ID] = true
		urls[b.URL] = true
	}
	cs := map[string]Collection{}
	names := map[string]bool{}
	for _, c := range a.Collections {
		if len(c.ID) != 32 || strings.Trim(c.ID, "0123456789abcdef") != "" || cs[c.ID].ID != "" || c.Name != strings.TrimSpace(c.Name) || c.Name == "" || len(c.Name) > 200 || names[fold(c.Name)] || (c.Kind != "manual" && c.Kind != "smart") {
			return result, errors.New("invalid collection")
		}
		if _, _, err = ruleClause(c.Rules); err != nil {
			return result, err
		}
		cs[c.ID] = c
		names[fold(c.Name)] = true
	}
	seenMembers := map[membership]bool{}
	for _, v := range a.Members {
		if !stories[v.Story] || cs[v.Collection].Kind != "manual" || seenMembers[v] {
			return result, errors.New("invalid membership")
		}
		seenMembers[v] = true
	}
	// Android's Go default temp directory (/data/local/tmp) is not app-writable.
	// Stage beside the explicit library path, inside its private storage boundary.
	if err := os.MkdirAll(filepath.Dir(l.Store.Path), 0700); err != nil {
		return result, err
	}
	temp, err := os.MkdirTemp(filepath.Dir(l.Store.Path), "sailune-import-*")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(temp)
	assets := map[string]Artwork{}
	roles := map[string]bool{}
	for _, art := range a.Artwork {
		key := fmt.Sprintf("%d/%s", art.StoryID, art.Role)
		if !stories[art.StoryID] || !validRole(art.Role) || !focal(art.X) || !focal(art.Y) || roles[key] {
			return result, errors.New("invalid artwork reference")
		}
		roles[key] = true
		limit := int64(1024 * 1024)
		if art.Role == "cover" {
			limit = 750 * 1024
		}
		entry := entries["assets/"+art.AssetID+".jpg"]
		if entry == nil {
			return result, errors.New("missing artwork asset")
		}
		data, e := zipBytes(entry, limit)
		if e != nil {
			return result, e
		}
		if assetHash(data) != art.AssetID {
			return result, errors.New("artwork checksum mismatch")
		}
		cfg, format, e := image.DecodeConfig(bytes.NewReader(data))
		if e != nil || format != "jpeg" || cfg.Width != art.Width || cfg.Height != art.Height || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 2000 || cfg.Height > 2000 {
			return result, errors.New("invalid optimized image")
		}
		if art.Role == "cover" && (cfg.Width > 1000 || cfg.Height > 1500) {
			return result, errors.New("cover dimensions exceed limits")
		}
		if _, ok := assets[art.AssetID]; !ok {
			img, _, e := image.Decode(bytes.NewReader(data))
			if e != nil {
				return result, e
			}
			scale := min(1.0, 480.0/float64(cfg.Width))
			thumb, e := encodeJPEG(sampleResize(img, 1, max(1, int(float64(cfg.Width)*scale)), max(1, int(float64(cfg.Height)*scale)), img.Bounds()), 80)
			if e != nil {
				return result, e
			}
			if e = os.WriteFile(filepath.Join(temp, art.AssetID), data, 0600); e != nil {
				return result, e
			}
			if e = os.WriteFile(filepath.Join(temp, art.AssetID+".thumb"), thumb, 0600); e != nil {
				return result, e
			}
			assets[art.AssetID] = art
		}
	}
	if len(entries) != len(assets)+2 {
		return result, errors.New("unreferenced archive assets")
	}
	err = l.Store.write(func(tx *sql.Tx) error {
		var next int64
		var count int
		if e := tx.QueryRow("SELECT next_id FROM library_meta").Scan(&next); e != nil {
			return e
		}
		if e := tx.QueryRow("SELECT (SELECT count(*) FROM bookmarks)+(SELECT count(*) FROM collections)").Scan(&count); e != nil {
			return e
		}
		if !merge && (count != 0 || next != 1) {
			return errors.New("restore requires a pristine library")
		}
		ids := map[int64]int64{}
		for _, b := range a.Bookmarks {
			old := b.ID
			if merge {
				var local int64
				e := tx.QueryRow("SELECT id FROM bookmarks WHERE url=?", b.URL).Scan(&local)
				if e == nil {
					ids[old] = local
					result.Skipped++
					continue
				}
				if !errors.Is(e, sql.ErrNoRows) {
					return e
				}
				if next == math.MaxInt64 {
					return errors.New("library ID limit reached")
				}
				b.ID = next
				next++
			}
			if e := saveBookmark(tx, b); e != nil {
				return e
			}
			ids[old] = b.ID
			result.Imported++
		}
		if !merge {
			next = a.NextID
		}
		if _, e := tx.Exec("UPDATE library_meta SET next_id=?", next); e != nil {
			return e
		}
		for _, c := range a.Collections {
			old, e := readCollection(tx, c.ID)
			if e == nil {
				if old.Name != c.Name || old.Kind != c.Kind || !rulesEqual(old.Rules, c.Rules) {
					result.Conflicts++
				}
				continue
			}
			if !errors.Is(e, ErrNotFound) {
				return e
			}
			name := c.Name
			for suffix := 1; ; suffix++ {
				var n int
				if e = tx.QueryRow("SELECT count(*) FROM collections WHERE name_key=?", fold(name)).Scan(&n); e != nil {
					return e
				}
				if n == 0 {
					break
				}
				base := c.Name
				for len(base) > 170 {
					rr := []rune(base)
					base = string(rr[:len(rr)-1])
				}
				name = fmt.Sprintf("%s (imported %d)", base, suffix)
			}
			rules, _ := json.Marshal(c.Rules)
			if _, e = tx.Exec("INSERT INTO collections VALUES(?,?,?,?,?)", c.ID, name, fold(name), c.Kind, string(rules)); e != nil {
				return e
			}
			result.Collections++
		}
		for _, v := range a.Members {
			c, e := readCollection(tx, v.Collection)
			if e != nil {
				return e
			}
			if c.Kind != "manual" {
				result.Conflicts++
				continue
			}
			if _, e = tx.Exec("INSERT OR IGNORE INTO collection_members VALUES(?,?)", v.Collection, ids[v.Story]); e != nil {
				return e
			}
		}
		for _, art := range a.Artwork {
			var existing string
			e := tx.QueryRow("SELECT asset_id FROM story_artwork WHERE bookmark_id=? AND role=?", ids[art.StoryID], art.Role).Scan(&existing)
			if e == nil {
				if existing != art.AssetID {
					result.Conflicts++
				}
				continue
			}
			if !errors.Is(e, sql.ErrNoRows) {
				return e
			}
			data, e := os.ReadFile(filepath.Join(temp, art.AssetID))
			if e != nil {
				return e
			}
			thumb, e := os.ReadFile(filepath.Join(temp, art.AssetID+".thumb"))
			if e != nil {
				return e
			}
			if _, e = tx.Exec("INSERT OR IGNORE INTO artwork_assets VALUES(?,?,?,?,?)", art.AssetID, art.Width, art.Height, data, thumb); e != nil {
				return e
			}
			if _, e = tx.Exec("INSERT INTO story_artwork VALUES(?,?,?,?,?)", ids[art.StoryID], art.Role, art.AssetID, art.X, art.Y); e != nil {
				return e
			}
			result.Artwork++
		}
		return nil
	})
	if err != nil {
		return ImportResult{}, err
	}
	return result, nil
}
func rulesEqual(a, b CollectionRules) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}
