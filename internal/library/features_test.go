package library

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }

type featureFetcher struct{ m Metadata }

func (f featureFetcher) Fetch(context.Context, string) (Metadata, error) { return f.m, nil }

func TestCustomizationsSurviveRefreshAndResetIndividually(t *testing.T) {
	l := testLibrary(t)
	b, err := l.Add(Bookmark{URL: "https://archiveofourown.org/works/12", Notes: "private", Metadata: &Metadata{Summary: "source", Words: 100, Chapters: 3}})
	if err != nil {
		t.Fatal(err)
	}
	added := time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)
	read := added.Add(time.Hour)
	p := Patch{Rating: ptr(5), ReviewNotes: ptr("great pacing"), Chapter: ptr(2), CreatedAt: &added, LastReadAt: &read,
		Overrides: &MetadataPatch{Summary: ptr("custom"), Words: ptr(500), Chapters: ptr(4), TotalChapters: ptr(8), Complete: ptr(false),
			Fandoms: ptr([]string{" Magic ", "magic"}), Tags: ptr([]string{"AU"}), Language: ptr("English"), Rating: ptr("Teen"), Published: ptr("2020-01-01"), Updated: ptr("2020-01-02")}}
	b, err = l.Update(b.ID, p)
	if err != nil {
		t.Fatal(err)
	}
	b, err = l.Refresh(context.Background(), b.ID, featureFetcher{Metadata{Summary: "new source", Words: 999, Chapters: 7}})
	if err != nil {
		t.Fatal(err)
	}
	m := b.EffectiveMetadata()
	if m.Summary != "custom" || m.Words != 500 || m.Chapters != 4 || !reflect.DeepEqual(m.Fandoms, []string{"Magic"}) || b.Metadata.Words != 999 {
		t.Fatalf("%+v", b)
	}
	if b.Rating != 5 || b.ReviewNotes != "great pacing" || b.Notes != "private" || !b.LastReadAt.Equal(read) || !b.CreatedAt.Equal(added) {
		t.Fatalf("personal data changed: %+v", b)
	}
	if p := b.ReadingProgress(); p.Percent != 50 || p.Unread != 2 {
		t.Fatalf("%+v", p)
	}
	b, err = l.Update(b.ID, Patch{ResetOverrides: ptr("words"), Overrides: &MetadataPatch{Summary: ptr("")}, ReviewNotes: ptr(""), Rating: ptr(0)})
	if err != nil {
		t.Fatal(err)
	}
	if m := b.EffectiveMetadata(); m.Words != 999 || m.Summary != "" || m.Chapters != 4 || b.Notes != "private" || b.ReviewNotes != "" || b.Rating != 0 {
		t.Fatalf("%+v %+v", b, m)
	}
	b, err = l.Update(b.ID, Patch{ResetOverrides: ptr("all")})
	if err != nil || b.EffectiveMetadata().Summary != "new source" {
		t.Fatalf("%+v %v", b, err)
	}
}

func TestInvalidCustomizationsAreAtomic(t *testing.T) {
	l := testLibrary(t)
	b, err := l.Add(Bookmark{URL: "https://fanfiction.net/s/1/1"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(l.Store.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []Patch{
		{Rating: ptr(6)}, {Rating: ptr(-1)}, {CreatedAt: ptr(time.Time{})}, {Overrides: &MetadataPatch{Words: ptr(-1)}},
		{Overrides: &MetadataPatch{Chapters: ptr(-1)}}, {Overrides: &MetadataPatch{Published: ptr("2020-13-01")}},
		{ResetOverrides: ptr("words,nope")},
	} {
		p.Notes = ptr("must not save")
		if _, err := l.Update(b.ID, p); err == nil {
			t.Fatalf("accepted %+v", p)
		}
		after, err := os.ReadFile(l.Store.Path)
		if err != nil || string(before) != string(after) {
			t.Fatal("failed update changed file", err)
		}
	}
}

func TestLastReadSemanticsAndOldLibrary(t *testing.T) {
	l := testLibrary(t)
	b, err := l.Add(Bookmark{URL: "https://fanfiction.net/s/1/1"})
	if err != nil {
		t.Fatal(err)
	}
	if !b.LastReadAt.IsZero() {
		t.Fatal("unread bookmark has last read")
	}
	b, err = l.Update(b.ID, Patch{Chapter: ptr(2)})
	if err != nil || b.LastReadAt.IsZero() {
		t.Fatal(b, err)
	}
	last := b.LastReadAt
	b, err = l.Update(b.ID, Patch{Notes: ptr("hello")})
	if err != nil || !b.LastReadAt.Equal(last) {
		t.Fatal(b, err)
	}
	b, err = l.Update(b.ID, Patch{Chapter: ptr(0)})
	if err != nil || !b.LastReadAt.IsZero() {
		t.Fatal(b, err)
	}
	old := `{"version":1,"next_id":2,"bookmarks":[{"id":1,"url":"https://archiveofourown.org/works/1","site":"ao3","work_id":"1","status":"planned","chapter":0,"tags":[]}]}`
	l = testLibrary(t)
	if _, err := l.Import(strings.NewReader(old), false); err != nil {
		t.Fatal(err)
	}
	b, err = l.Get(1)
	if err != nil || b.Rating != 0 || !b.LastReadAt.IsZero() || b.ReadingProgress().Known {
		t.Fatal(b, err)
	}
}

func TestListDiscoveryAndSorting(t *testing.T) {
	l := testLibrary(t)
	for i, b := range []Bookmark{
		{URL: "https://archiveofourown.org/works/1", Title: "Zulu", Author: "Alice", Rating: 5, ReviewNotes: "Beautiful ending", Metadata: &Metadata{Words: 100, Chapters: 4, Complete: true, Fandoms: []string{"Magic"}, Language: "English", Tags: []string{"AU"}}},
		{URL: "https://fanfiction.net/s/2/1", Title: "Alpha", Author: "Bob", Rating: 3, Chapter: 2, Metadata: &Metadata{Words: 200, Chapters: 2, Language: "English"}},
		{URL: "https://archiveofourown.org/works/3", Title: "Beta", Rating: 5},
	} {
		if _, err := l.Add(b); err != nil {
			t.Fatal(i, err)
		}
	}
	cases := []struct {
		f   Filter
		ids []int64
	}{
		{Filter{Query: "ALICE ending"}, []int64{1}},
		{Filter{Author: "ali", Fandom: "magic", Language: "english", SourceTag: "au", Complete: ptr(true), Unread: true, MinRating: 4, MinWords: 50, MaxWords: 150}, []int64{1}},
		{Filter{Complete: ptr(false)}, []int64{2}},
		{Filter{Sort: "title"}, []int64{2, 3, 1}},
		{Filter{Sort: "rating", Desc: true}, []int64{1, 3, 2}},
		{Filter{Sort: "words", Desc: true, Offset: 1, Limit: 1}, []int64{1}},
		{Filter{Sort: "progress", Desc: true}, []int64{2, 1, 3}},
		{Filter{Offset: 99}, []int64{}},
	}
	for _, tc := range cases {
		got, err := l.List(tc.f)
		if err != nil {
			t.Fatal(err)
		}
		ids := []int64{}
		for _, b := range got {
			ids = append(ids, b.ID)
		}
		if !reflect.DeepEqual(ids, tc.ids) {
			t.Fatalf("%+v: got %v want %v", tc.f, ids, tc.ids)
		}
	}
	for _, f := range []Filter{{Sort: "bad"}, {MinRating: 6}, {MinWords: -1}, {MinWords: 10, MaxWords: 5}, {Offset: -1}, {Limit: -1}} {
		if _, err := l.List(f); err == nil {
			t.Fatalf("accepted %+v", f)
		}
	}
	if _, err := l.Update(1, Patch{Overrides: &MetadataPatch{Words: ptr(900), Fandoms: ptr([]string{"New"})}}); err != nil {
		t.Fatal(err)
	}
	got, err := l.List(Filter{Fandom: "new", MinWords: 800})
	if err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
}

func TestOpenResumeDestinationsAndNoMutation(t *testing.T) {
	l := testLibrary(t)
	for _, b := range []Bookmark{
		{URL: "https://archiveofourown.org/works/1", Chapter: 1, Metadata: &Metadata{Chapters: 3, ChapterIDs: []string{"900", "120", "700"}}},
		{URL: "https://fanfiction.net/s/2/1", Chapter: 2, Metadata: &Metadata{Chapters: 2}},
		{URL: "https://archiveofourown.org/works/3", Chapter: 1},
		{URL: "https://fanfiction.net/s/4/1", Chapter: 4},
		{URL: "https://archiveofourown.org/works/5", Metadata: &Metadata{Chapters: 2, ChapterIDs: []string{"https://evil.test", "2"}}},
	} {
		if _, err := l.Add(b); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := os.ReadFile(l.Store.Path)
	if u, err := l.ResumeURL(1); err != nil || u != "https://archiveofourown.org/works/1/chapters/120" {
		t.Fatal(u, err)
	}
	if _, err := l.ResumeURL(2); err == nil || !strings.Contains(err.Error(), "caught up") {
		t.Fatal(err)
	}
	if _, err := l.ResumeURL(3); err == nil {
		t.Fatal("missing index accepted")
	}
	if u, err := l.ResumeURL(4); err != nil || u != "https://www.fanfiction.net/s/4/5" {
		t.Fatal(u, err)
	}
	if _, err := l.OpenURL(5, 1); err == nil {
		t.Fatal("bad chapter ID accepted")
	}
	if u, err := l.OpenURL(3, 1); err != nil || u != "https://archiveofourown.org/works/3" {
		t.Fatal(u, err)
	}
	for _, tc := range [][2]int64{{1, -1}, {1, 4}, {999, 0}} {
		if _, err := l.OpenURL(tc[0], int(tc[1])); err == nil {
			t.Fatal(tc)
		}
	}
	after, _ := os.ReadFile(l.Store.Path)
	if string(before) != string(after) {
		t.Fatal("opening changed progress")
	}
}
