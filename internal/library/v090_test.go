package library

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func sampleArt(t *testing.T) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 120, 90))
	for y := 0; y < 90; y++ {
		for x := 0; x < 120; x++ {
			im.Set(x, y, color.RGBA{uint8(x * 2), uint8(y * 2), 100, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestCollectionsArtworkArchive(t *testing.T) {
	l := Library{Store: Store{Path: filepath.Join(t.TempDir(), "library.db")}}
	a, e := l.Add(Bookmark{URL: "https://archiveofourown.org/works/123", Tags: []string{"Angst", "Hope"}})
	if e != nil {
		t.Fatal(e)
	}
	b, e := l.Add(Bookmark{URL: "https://www.fanfiction.net/s/456/1", Tags: []string{"Hope"}})
	if e != nil {
		t.Fatal(e)
	}
	c, e := l.SaveCollection(Collection{Name: "Trauma", Kind: "manual"})
	if e != nil {
		t.Fatal(e)
	}
	if e = l.SetMembership(c.ID, []int64{a.ID, b.ID}, false); e != nil {
		t.Fatal(e)
	}
	if e = l.SetMembership(c.ID, []int64{a.ID, 999}, true); e == nil {
		t.Fatal("invalid bulk update accepted")
	}
	if n, e := l.Count(Filter{Collection: c.ID}); e != nil || n != 2 {
		t.Fatalf("atomic membership count %d %v", n, e)
	}
	smart, e := l.SaveCollection(Collection{Name: "Smart", Kind: "smart", Rules: CollectionRules{Personal: TagRule{Tags: []string{"angst", "hope"}, All: true}}})
	if e != nil {
		t.Fatal(e)
	}
	if n, e := l.Count(Filter{Collection: smart.ID}); e != nil || n != 1 {
		t.Fatalf("smart count %d %v", n, e)
	}
	rows, e := l.List(Filter{Collection: c.ID, Limit: 1, Offset: 1})
	if e != nil || len(rows) != 1 || rows[0].ID != b.ID {
		t.Fatalf("pagination %+v %v", rows, e)
	}
	art, e := l.SetArtwork(a.ID, "cover", bytes.NewReader(sampleArt(t)), .5, .5)
	if e != nil {
		t.Fatal(e)
	}
	if art.Width*3 != art.Height*2 {
		t.Fatalf("portrait not 2:3: %+v", art)
	}
	if _, e = l.SetArtwork(a.ID, "background", bytes.NewReader(sampleArt(t)), .2, .8); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "backup.zip")
	if e = l.ExportArchiveFile(path); e != nil {
		t.Fatal(e)
	}
	restored := Library{Store: Store{Path: filepath.Join(t.TempDir(), "restored.db")}}
	res, e := restored.ImportBackupFile(path, false)
	if e != nil || res.Imported != 2 || res.Artwork != 2 || res.Collections != 2 {
		t.Fatalf("restore %+v %v", res, e)
	}
	data, _ := l.ArtworkBytes(art.AssetID, false)
	data2, e := restored.ArtworkBytes(art.AssetID, false)
	if e != nil || !bytes.Equal(data, data2) {
		t.Fatal("image roundtrip changed")
	}
	merge := Library{Store: Store{Path: filepath.Join(t.TempDir(), "merge.db")}}
	_, _ = merge.Add(Bookmark{URL: "https://archiveofourown.org/works/999"})
	existing, _ := merge.Add(Bookmark{URL: a.URL})
	res, e = merge.ImportBackupFile(path, true)
	if e != nil || res.Skipped != 1 || res.Imported != 1 {
		t.Fatalf("merge %+v %v", res, e)
	}
	arts, e := merge.Artwork(existing.ID)
	if e != nil || len(arts) != 2 {
		t.Fatal("art ID remapping failed", e)
	}
	for i := 0; i < 2; i++ {
		res, e = merge.ImportBackupFile(path, true)
		if e != nil || res.Imported != 0 || res.Collections != 0 || res.Artwork != 0 {
			t.Fatalf("not idempotent %+v %v", res, e)
		}
	}
	if e = restored.DeleteCollection(c.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = restored.Get(a.ID); e != nil {
		t.Fatal("collection deleted story")
	}
	if e = restored.Delete(a.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = restored.ArtworkBytes(art.AssetID, false); e == nil {
		t.Fatal("orphan asset retained")
	}
}
func TestArchiveRejectsTraversalAndCorruption(t *testing.T) {
	l := Library{Store: Store{Path: filepath.Join(t.TempDir(), "lib")}}
	for _, name := range []string{"../escape", "assets/../../escape", "manifest.json"} {
		var b bytes.Buffer
		z := zip.NewWriter(&b)
		w, _ := z.Create(name)
		w.Write([]byte("bad"))
		z.Close()
		p := filepath.Join(t.TempDir(), "bad.zip")
		os.WriteFile(p, b.Bytes(), 0600)
		if _, e := l.ImportBackupFile(p, true); e == nil {
			t.Fatal("accepted", name)
		}
	}
	if _, e := l.SetArtwork(1, "cover", bytes.NewReader([]byte("not an image")), .5, .5); e == nil {
		t.Fatal("accepted invalid image")
	}
	if _, e := l.SaveCollection(Collection{Name: " ", Kind: "manual"}); e == nil {
		t.Fatal("accepted empty name")
	}
}

func TestSchemaMigrationPreservesPersonalDataAndRejectsFuture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	l := Library{Store: Store{Path: path}}
	// Initialize the actual previous schema before the first v0.9 operation.
	if e := l.Store.initialize(path); e != nil {
		t.Fatal(e)
	}
	db, e := sqliteConnect(path)
	if e != nil {
		t.Fatal(e)
	}
	var v int
	if e = db.QueryRow("PRAGMA user_version").Scan(&v); e != nil || v != 1 {
		t.Fatal(v, e)
	}
	db.Close()
	b, e := l.Add(Bookmark{URL: "https://archiveofourown.org/works/900", Notes: "keep", Tags: []string{"Kelvin"}})
	if e != nil {
		t.Fatal(e)
	}
	db, e = sqliteConnect(path)
	if e != nil {
		t.Fatal(e)
	}
	db.QueryRow("PRAGMA user_version").Scan(&v)
	if v != 2 {
		t.Fatal("not migrated")
	}
	db.Close()
	c, e := l.SaveCollection(Collection{Name: "Unicode", Kind: "smart", Rules: CollectionRules{Personal: TagRule{Tags: []string{"kelvin"}}}})
	if e != nil {
		t.Fatal(e)
	}
	if n, e := l.Count(Filter{Collection: c.ID}); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	manual, e := l.SaveCollection(Collection{Name: "Keep", Kind: "manual"})
	if e != nil {
		t.Fatal(e)
	}
	if e = l.SetMembership(manual.ID, []int64{b.ID}, false); e != nil {
		t.Fatal(e)
	}
	art, e := l.SetArtwork(b.ID, "cover", bytes.NewReader(sampleArt(t)), .5, .5)
	if e != nil {
		t.Fatal(e)
	}
	chapter := 4
	if _, e = l.Update(b.ID, Patch{Chapter: &chapter}); e != nil {
		t.Fatal(e)
	}
	saved, e := l.Get(b.ID)
	if e != nil || saved.Notes != "keep" {
		t.Fatal("personal data lost", e)
	}
	if n, e := l.Count(Filter{Collection: manual.ID}); e != nil || n != 1 {
		t.Fatal("membership lost on update", e)
	}
	if data, e := l.ArtworkBytes(art.AssetID, false); e != nil || len(data) == 0 {
		t.Fatal("art lost on update", e)
	}
	db, _ = sqliteConnect(path)
	db.Exec("PRAGMA user_version=999")
	db.Close()
	if _, e = l.Get(b.ID); e == nil {
		t.Fatal("future schema accepted")
	}
}

func TestFailedArchiveImportIsAtomic(t *testing.T) {
	l := Library{Store: Store{Path: filepath.Join(t.TempDir(), "lib")}}
	b, e := l.Add(Bookmark{URL: "https://archiveofourown.org/works/456"})
	if e != nil {
		t.Fatal(e)
	}
	a, e := l.SetArtwork(b.ID, "background", bytes.NewReader(sampleArt(t)), .5, .5)
	if e != nil {
		t.Fatal(e)
	}
	var buf bytes.Buffer
	if e = l.ExportArchive(&buf); e != nil {
		t.Fatal(e)
	}
	zr, e := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if e != nil {
		t.Fatal(e)
	}
	var bad bytes.Buffer
	zw := zip.NewWriter(&bad)
	for _, f := range zr.File {
		data, e := zipBytes(f, MaxArchiveBytes)
		if e != nil {
			t.Fatal(e)
		}
		if f.Name == "assets/"+a.AssetID+".jpg" {
			data[len(data)/2] ^= 1
		}
		w, _ := zw.Create(f.Name)
		w.Write(data)
	}
	zw.Close()
	target := Library{Store: Store{Path: filepath.Join(t.TempDir(), "target")}}
	original, _ := target.Add(Bookmark{URL: "https://archiveofourown.org/works/999", Notes: "untouched"})
	path := filepath.Join(t.TempDir(), "corrupt.zip")
	os.WriteFile(path, bad.Bytes(), 0600)
	if _, e = target.ImportBackupFile(path, true); e == nil {
		t.Fatal("corrupt archive accepted")
	}
	rows, e := target.List(Filter{})
	if e != nil || len(rows) != 1 || rows[0].ID != original.ID || rows[0].Notes != "untouched" {
		t.Fatal("failed import changed library", e)
	}
}
