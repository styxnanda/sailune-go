package library

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// CollectionRules use AND between groups and Any/All within each tag group.
type TagRule struct {
	Tags []string `json:"tags"`
	All  bool     `json:"all"`
}
type CollectionRules struct {
	Personal TagRule `json:"personal"`
	Source   TagRule `json:"source"`
	Site     Site    `json:"site,omitempty"`
	Status   Status  `json:"status,omitempty"`
	Fandom   string  `json:"fandom,omitempty"`
}
type Collection struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Kind  string          `json:"kind"`
	Rules CollectionRules `json:"rules"`
	Count int             `json:"count"`
}

const additions = `
CREATE TABLE collections(id TEXT PRIMARY KEY, name TEXT NOT NULL, name_key TEXT NOT NULL UNIQUE, kind TEXT NOT NULL CHECK(kind IN ('manual','smart')), rules TEXT NOT NULL);
CREATE TABLE collection_members(collection_id TEXT NOT NULL REFERENCES collections(id) ON DELETE CASCADE, bookmark_id INTEGER NOT NULL REFERENCES bookmarks(id) ON DELETE CASCADE, PRIMARY KEY(collection_id,bookmark_id));
CREATE INDEX collection_story ON collection_members(bookmark_id,collection_id);
CREATE TABLE artwork_assets(id TEXT PRIMARY KEY, width INTEGER NOT NULL, height INTEGER NOT NULL, data BLOB NOT NULL, thumbnail BLOB NOT NULL);
CREATE TABLE story_artwork(bookmark_id INTEGER NOT NULL REFERENCES bookmarks(id) ON DELETE CASCADE, role TEXT NOT NULL CHECK(role IN ('cover','background')), asset_id TEXT NOT NULL REFERENCES artwork_assets(id), x REAL NOT NULL DEFAULT 0.5, y REAL NOT NULL DEFAULT 0.5, PRIMARY KEY(bookmark_id,role));
CREATE INDEX artwork_refs ON story_artwork(asset_id);
PRAGMA user_version=2;
`

func ruleClause(r CollectionRules) (string, []any, error) {
	if r.Site != "" && r.Site != AO3 && r.Site != FFN {
		return "", nil, errors.New("invalid rule site")
	}
	if r.Status != "" && !r.Status.Valid() {
		return "", nil, errors.New("invalid rule status")
	}
	clauses := []string{"1=1"}
	args := []any{}
	for _, g := range []struct {
		kind string
		rule TagRule
	}{{"tag", r.Personal}, {"source-tag", r.Source}} {
		if len(g.rule.Tags) > 100 {
			return "", nil, errors.New("at most 100 tags per rule")
		}
		parts := []string{}
		for _, tag := range g.rule.Tags {
			if strings.TrimSpace(tag) == "" {
				return "", nil, errors.New("rule tags cannot be empty")
			}
			parts = append(parts, "id IN (SELECT bookmark_id FROM facets WHERE kind=? AND value=?)")
			args = append(args, g.kind, fold(strings.TrimSpace(tag)))
		}
		if len(parts) > 0 {
			join := " OR "
			if g.rule.All {
				join = " AND "
			}
			clauses = append(clauses, "("+strings.Join(parts, join)+")")
		}
	}
	if r.Site != "" {
		clauses = append(clauses, "site=?")
		args = append(args, r.Site)
	}
	if r.Status != "" {
		clauses = append(clauses, "status=?")
		args = append(args, r.Status)
	}
	if r.Fandom != "" {
		clauses = append(clauses, "id IN (SELECT bookmark_id FROM facets WHERE kind='fandom' AND value=?)")
		args = append(args, fold(strings.TrimSpace(r.Fandom)))
	}
	return strings.Join(clauses, " AND "), args, nil
}
func readCollection(q interface{ QueryRow(string, ...any) *sql.Row }, id string) (Collection, error) {
	var c Collection
	var raw string
	err := q.QueryRow("SELECT id,name,kind,rules FROM collections WHERE id=?", id).Scan(&c.ID, &c.Name, &c.Kind, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	err = json.Unmarshal([]byte(raw), &c.Rules)
	return c, err
}
func (l Library) SaveCollection(c Collection) (Collection, error) {
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" || len(c.Name) > 200 {
		return c, errors.New("collection name must contain 1–200 bytes")
	}
	if c.Kind != "manual" && c.Kind != "smart" {
		return c, errors.New("collection kind must be manual or smart")
	}
	if _, _, err := ruleClause(c.Rules); err != nil {
		return c, err
	}
	create := c.ID == ""
	if create {
		var b [16]byte
		if _, err := rand.Read(b[:]); err != nil {
			return c, err
		}
		c.ID = hex.EncodeToString(b[:])
	}
	raw, _ := json.Marshal(c.Rules)
	err := l.Store.write(func(tx *sql.Tx) error {
		if !create {
			old, err := readCollection(tx, c.ID)
			if err != nil {
				return err
			}
			if old.Kind != c.Kind {
				return errors.New("collection type cannot change; create another collection")
			}
		}
		_, err := tx.Exec("INSERT INTO collections(id,name,name_key,kind,rules) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,name_key=excluded.name_key,rules=excluded.rules", c.ID, c.Name, fold(c.Name), c.Kind, string(raw))
		return err
	})
	return c, err
}
func (l Library) Collections() ([]Collection, error) {
	result := []Collection{}
	db, err := l.Store.open(false)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query("SELECT id,name,kind,rules FROM collections ORDER BY name_key,id")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c Collection
		var raw string
		if err := rows.Scan(&c.ID, &c.Name, &c.Kind, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &c.Rules); err != nil {
			rows.Close()
			return nil, err
		}
		result = append(result, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range result {
		c := &result[i]
		clause, args, e := collectionClause(*c)
		if e != nil {
			return nil, e
		}
		if err := db.QueryRow("SELECT count(*) FROM bookmarks WHERE "+clause, args...).Scan(&c.Count); err != nil {
			return nil, err
		}
	}
	return result, nil
}
func collectionClause(c Collection) (string, []any, error) {
	if c.Kind == "smart" {
		return ruleClause(c.Rules)
	}
	return "id IN (SELECT bookmark_id FROM collection_members WHERE collection_id=?)", []any{c.ID}, nil
}
func (l Library) DeleteCollection(id string) error {
	return l.Store.write(func(tx *sql.Tx) error {
		res, err := tx.Exec("DELETE FROM collections WHERE id=?", id)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return ErrNotFound
		}
		return nil
	})
}
func (l Library) SetMembership(id string, stories []int64, remove bool) error {
	if len(stories) > 10000 {
		return errors.New("select at most 10000 stories")
	}
	return l.Store.write(func(tx *sql.Tx) error {
		c, err := readCollection(tx, id)
		if err != nil {
			return err
		}
		if c.Kind != "manual" {
			return errors.New("smart membership is controlled by rules")
		}
		for _, story := range stories {
			var n int
			if err := tx.QueryRow("SELECT count(*) FROM bookmarks WHERE id=?", story).Scan(&n); err != nil {
				return err
			}
			if n == 0 {
				return fmt.Errorf("%w: %d", ErrNotFound, story)
			}
			if remove {
				_, err = tx.Exec("DELETE FROM collection_members WHERE collection_id=? AND bookmark_id=?", id, story)
			} else {
				_, err = tx.Exec("INSERT OR IGNORE INTO collection_members VALUES(?,?)", id, story)
			}
			if err != nil {
				return err
			}
		}
		return nil
	})
}
func (l Library) Tags(kind string) ([]string, error) {
	if kind != "tag" && kind != "source-tag" && kind != "fandom" {
		return nil, errors.New("invalid tag kind")
	}
	result := []string{}
	db, err := l.Store.open(false)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	defer db.Close()
	expression := "json_extract(payload,'$.tags')"
	switch kind {
	case "source-tag":
		expression = "COALESCE(json_extract(payload,'$.overrides.tags'),json_extract(payload,'$.metadata.tags'))"
	case "fandom":
		expression = "COALESCE(json_extract(payload,'$.overrides.fandoms'),json_extract(payload,'$.metadata.fandoms'))"
	}
	rows, err := db.Query("SELECT DISTINCT j.value FROM bookmarks, json_each(" + expression + ") AS j WHERE j.type='text' ORDER BY j.value")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		if !seen[fold(s)] {
			result = append(result, s)
			seen[fold(s)] = true
		}
	}
	return result, rows.Err()
}
