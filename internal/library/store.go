package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"

	"github.com/styxnanda/sailune-go/internal/model"
	_ "modernc.org/sqlite"
)

// Store is a local SQLite library. Each operation owns and closes its connection.
// Never cloud-sync an open database; use ExportFile/ImportFile for transfers.
type Store struct{ Path string }

const schema = `
PRAGMA application_id = 1396787532;
PRAGMA user_version = 1;
CREATE TABLE library_meta (singleton INTEGER PRIMARY KEY CHECK(singleton=1), next_id INTEGER NOT NULL CHECK(next_id>0));
INSERT INTO library_meta VALUES(1,1);
CREATE TABLE bookmarks (
 id INTEGER PRIMARY KEY CHECK(id>0), url TEXT NOT NULL UNIQUE,
 site TEXT NOT NULL, status TEXT NOT NULL, title TEXT NOT NULL, author TEXT NOT NULL,
 added TEXT NOT NULL, last_read TEXT NOT NULL, updated TEXT NOT NULL, source_updated TEXT NOT NULL,
 rating INTEGER NOT NULL, words INTEGER NOT NULL, complete INTEGER, unread INTEGER NOT NULL,
 progress INTEGER NOT NULL, language TEXT NOT NULL, author_search TEXT NOT NULL,
 search_text TEXT NOT NULL, payload TEXT NOT NULL CHECK(json_valid(payload))
);
CREATE TABLE facets (bookmark_id INTEGER NOT NULL REFERENCES bookmarks(id) ON DELETE CASCADE,
 kind TEXT NOT NULL, value TEXT NOT NULL, PRIMARY KEY(bookmark_id,kind,value));
CREATE INDEX facets_lookup ON facets(kind,value,bookmark_id);
CREATE INDEX bookmarks_site_status ON bookmarks(site,status);
CREATE INDEX bookmarks_status ON bookmarks(status);
CREATE INDEX bookmarks_added ON bookmarks(added,id);
CREATE INDEX bookmarks_last_read ON bookmarks(last_read,id);
CREATE INDEX bookmarks_title ON bookmarks(title,id);
CREATE INDEX bookmarks_rating ON bookmarks(rating,id);
CREATE INDEX bookmarks_words ON bookmarks(words,id);
CREATE INDEX bookmarks_updated ON bookmarks(updated,id);
CREATE INDEX bookmarks_author ON bookmarks(author,id);
CREATE INDEX bookmarks_source_updated ON bookmarks(source_updated,id);
CREATE INDEX bookmarks_progress ON bookmarks(progress,id);
`

func sqliteConnect(path string) (*sql.DB, error) {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	// SQLite requires file:///C:/...; file://C:/... treats the drive as a host.
	if runtime.GOOS == "windows" {
		u.Path = "/" + u.Path
	}
	q := url.Values{"mode": {"rw"}, "_pragma": {"busy_timeout(5000)", "foreign_keys(1)", "synchronous(FULL)", "secure_delete(ON)"}, "_txlock": {"immediate"}}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func (s Store) open(create bool) (*sql.DB, error) {
	if s.Path == "" {
		return nil, errors.New("library path is empty")
	}
	path, err := filepath.Abs(s.Path)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && create {
		dir := filepath.Dir(path)
		_, dirErr := os.Stat(dir)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, err
		}
		defaultPath, _ := DefaultLibraryPath()
		if errors.Is(dirErr, os.ErrNotExist) || dir == filepath.Dir(defaultPath) {
			if err := privateDirectory(dir); err != nil {
				return nil, err
			}
		}
		if err := s.initialize(path); err != nil {
			return nil, err
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("library must be a regular file, not a symlink")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	header := make([]byte, 16)
	_, err = io.ReadFull(f, header)
	f.Close()
	if err != nil || string(header) != "SQLite format 3\x00" {
		return nil, errors.New("not a Sailune SQLite library; migrate legacy JSON with: sailune --data NEW.sqlite3 migrate OLD.json (source left untouched)")
	}
	if err := privateFile(path); err != nil {
		return nil, fmt.Errorf("protect library: %w", err)
	}
	db, err := sqliteConnect(path)
	if err != nil {
		return nil, err
	}
	var version, app int
	if err = db.QueryRow("PRAGMA application_id").Scan(&app); err == nil {
		err = db.QueryRow("PRAGMA user_version").Scan(&version)
	}
	if err != nil || app != 1396787532 || version != 1 {
		db.Close()
		return nil, errors.New("invalid or unsupported Sailune SQLite schema (file left untouched)")
	}
	return db, nil
}

// Publish a fully initialized database without replacing an existing file.
func (s Store) initialize(path string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sailune-init-*.sqlite3")
	if err != nil {
		return err
	}
	name := tmp.Name()
	tmp.Close()
	defer os.Remove(name)
	if err := privateFile(name); err != nil {
		return err
	}
	db, err := sqliteConnect(name)
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err == nil {
		_, err = tx.Exec(schema)
		if err == nil {
			err = tx.Commit()
		} else {
			tx.Rollback()
		}
	}
	closeErr := db.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Link(name, path); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return nil
}

func (s Store) write(fn func(*sql.Tx) error) error {
	db, err := s.open(true)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

type rowScanner interface{ Scan(...any) error }

func scanBookmark(row rowScanner) (Bookmark, error) {
	var raw string
	if err := row.Scan(&raw); err != nil {
		return Bookmark{}, err
	}
	var b Bookmark
	if err := json.Unmarshal([]byte(raw), &b); err != nil {
		return b, fmt.Errorf("invalid stored bookmark: %w", err)
	}
	return b, nil
}
func getBookmark(tx *sql.Tx, id int64) (Bookmark, error) {
	b, err := scanBookmark(tx.QueryRow("SELECT payload FROM bookmarks WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return b, fmt.Errorf("%w: %d", ErrNotFound, id)
	}
	return b, err
}
func (s Store) mutate(id int64, fn func(*Bookmark) error) (Bookmark, error) {
	var b Bookmark
	err := s.write(func(tx *sql.Tx) error {
		var err error
		b, err = getBookmark(tx, id)
		if err != nil {
			return err
		}
		if err := fn(&b); err != nil {
			return err
		}
		if err := model.Validate(b); err != nil {
			return err
		}
		b.UpdatedAt = time.Now().UTC()
		return saveBookmark(tx, b)
	})
	return b, err
}

// Fold exact facets using Unicode simple-fold equivalence, matching EqualFold.
func fold(s string) string {
	return strings.Map(func(r rune) rune {
		smallest := r
		for n := unicode.SimpleFold(r); n != r; n = unicode.SimpleFold(n) {
			if n < smallest {
				smallest = n
			}
		}
		return smallest
	}, s)
}
func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z") }
func saveBookmark(tx *sql.Tx, b Bookmark) error {
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	m, p := b.EffectiveMetadata(), b.ReadingProgress()
	var complete any
	if b.Metadata != nil || b.Overrides != nil && b.Overrides.Complete != nil {
		complete = m.Complete
	}
	search := strings.ToLower(strings.Join([]string{b.Title, b.Author, b.URL, b.Notes, b.ReviewNotes, strings.Join(b.Tags, " "), m.Title, strings.Join(m.Authors, " "), m.Summary, strings.Join(m.Fandoms, " "), strings.Join(m.Tags, " "), m.Language, m.Rating}, "\n"))
	_, err = tx.Exec(`INSERT INTO bookmarks VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
 ON CONFLICT(id) DO UPDATE SET url=excluded.url,site=excluded.site,status=excluded.status,title=excluded.title,
 author=excluded.author,added=excluded.added,last_read=excluded.last_read,updated=excluded.updated,
 source_updated=excluded.source_updated,rating=excluded.rating,words=excluded.words,complete=excluded.complete,
 unread=excluded.unread,progress=excluded.progress,language=excluded.language,author_search=excluded.author_search,
 search_text=excluded.search_text,payload=excluded.payload`, b.ID, b.URL, b.Site, b.Status, strings.ToLower(b.Title), strings.ToLower(b.Author), stamp(b.CreatedAt), stamp(b.LastReadAt), stamp(b.UpdatedAt), m.Updated, b.Rating, m.Words, complete, p.Unread, p.Percent, fold(m.Language), strings.ToLower(b.Author+"\n"+strings.Join(m.Authors, "\n")), search, string(raw))
	if err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM facets WHERE bookmark_id=?", b.ID); err != nil {
		return err
	}
	for kind, values := range map[string][]string{"tag": b.Tags, "source-tag": m.Tags, "fandom": m.Fandoms} {
		for _, v := range values {
			if _, err = tx.Exec("INSERT OR IGNORE INTO facets VALUES(?,?,?)", b.ID, kind, fold(v)); err != nil {
				return err
			}
		}
	}
	return nil
}
