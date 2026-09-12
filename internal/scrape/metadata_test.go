package scrape

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T, site Site) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + string(site) + ".html")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestMetadataParsers(t *testing.T) {
	a, err := ParseMetadata(AO3, strings.NewReader(fixture(t, AO3)))
	if err != nil {
		t.Fatal(err)
	}
	if a.Title != "Stars & Stories" || len(a.Authors) != 2 || a.Summary != "A small adventure. Another paragraph." || a.Words != 12345 || a.Chapters != 3 || a.TotalChapters != 5 || a.Complete || a.Language != "English" || len(a.Fandoms) != 1 || len(a.Tags) != 3 || a.Published != "2026-01-01" || a.Updated != "2026-02-01" {
		t.Fatalf("AO3: %+v", a)
	}
	f, err := ParseMetadata(FFN, strings.NewReader(fixture(t, FFN)))
	if err != nil {
		t.Fatal(err)
	}
	if f.Title != "A & B" || len(f.Authors) != 1 || f.Authors[0] != "The Writer" || f.Summary != "A story about friendship." || f.Chapters != 2 || f.Words != 4321 || !f.Complete || f.TotalChapters != 2 || f.Language != "English" || f.Rating != "Fiction T" || len(f.Fandoms) != 1 || len(f.Tags) != 2 || f.Published != "2026-01-01" || f.Updated != "2026-01-02" {
		t.Fatalf("FFN: %+v", f)
	}
	for _, chapters := range []string{"1/1", "3/?"} {
		page := strings.Replace(fixture(t, AO3), "3/5", chapters, 1)
		m, err := ParseMetadata(AO3, strings.NewReader(page))
		if err != nil || m.Complete != (chapters == "1/1") {
			t.Fatalf("chapters %s: %+v %v", chapters, m, err)
		}
	}
	page := strings.Replace(fixture(t, FFN), "Chapters: 2 - ", "", 1)
	page = strings.Replace(page, "Status: Complete - ", "", 1)
	m, err := ParseMetadata(FFN, strings.NewReader(page))
	if err != nil || m.Chapters != 1 || m.Complete || m.TotalChapters != 0 {
		t.Fatalf("one shot: %+v %v", m, err)
	}
}

func TestMetadataRejectsNonWorkPages(t *testing.T) {
	for _, tc := range []struct {
		page string
		want error
	}{
		{`<title>Just a moment...</title><form id="challenge-form"></form>`, ErrChallenge},
		{`<title>Log In | Archive of Our Own</title><form action="/users/login"></form>`, ErrLoginRequired},
		{`<title>FanFiction</title><p>Story Not Found</p>`, ErrWorkUnavailable},
		{`<title>A normal home page</title><p>No work header</p>`, ErrMetadata},
	} {
		for _, site := range []Site{AO3, FFN} {
			if _, err := ParseMetadata(site, strings.NewReader(tc.page)); !errors.Is(err, tc.want) {
				t.Errorf("%s: %v, want %v", site, err, tc.want)
			}
		}
	}
	page := `<div id="workskin"><div class="preface"><h2 class="title">Anonymous work</h2><h3 class="byline">Anonymous</h3></div></div>`
	m, err := ParseMetadata(AO3, strings.NewReader(page))
	if err != nil || len(m.Authors) != 1 || m.Authors[0] != "Anonymous" {
		t.Fatalf("anonymous: %+v %v", m, err)
	}
	m, err = ParseMetadata(AO3, strings.NewReader(strings.Replace(page, "Anonymous work", "script", 1)))
	if err != nil || m.Title != "script" {
		t.Fatalf("literal title: %+v %v", m, err)
	}
}

func TestFFNNestedWorkHeader(t *testing.T) {
	page, err := os.ReadFile("testdata/ffn-nested-header.html")
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseMetadata(FFN, strings.NewReader(string(page)))
	if err != nil || m.Title != "Example story" || len(m.Authors) != 1 || m.Authors[0] != "Example Writer" || m.Chapters != 21 || m.Words != 42000 || m.Summary != "An example summary." || m.Updated != "2026-09-10" || m.Published != "2026-05-16" {
		t.Fatalf("nested header: %+v %v", m, err)
	}
	for _, part := range []string{`<b class="xcontrast_txt">Example story</b>`, `<a class="xcontrast_txt" href="/u/123/ExampleWriter">Example Writer</a>`} {
		_, err := ParseMetadata(FFN, strings.NewReader(strings.Replace(string(page), part, "", 1)))
		if !errors.Is(err, ErrMetadata) || !strings.Contains(err.Error(), "missing work") {
			t.Fatalf("missing header diagnostics: %v", err)
		}
	}
}

func TestAO3ChapterIndex(t *testing.T) {
	data, err := os.ReadFile("testdata/ao3.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		options string
		count   int
	}{
		{`<option value="900">1. First</option><option value="120">2. Second</option><option value="700">3. Third</option>`, 3},
		{`<option value="900">1</option><option value="https://evil.test">2</option>`, 0},
	} {
		page := strings.Replace(string(data), "</body>", `<select id="selected_id">`+tc.options+`</select></body>`, 1)
		m, err := ParseMetadata(AO3, strings.NewReader(page))
		if err != nil || len(m.ChapterIDs) != tc.count {
			t.Fatalf("%+v %v", m.ChapterIDs, err)
		}
		if tc.count > 0 && m.ChapterIDs[1] != "120" {
			t.Fatal(m.ChapterIDs)
		}
	}
}

func TestAO3EntireWorkChapterIndex(t *testing.T) {
	data, err := os.ReadFile("testdata/ao3.html")
	if err != nil {
		t.Fatal(err)
	}
	chapters := `<div id="chapters"><div class="chapter" id="chapter-1"><div class="chapter preface"><h3 class="title"><a href="/works/1/chapters/900">Chapter 1</a></h3></div><div class="userstuff"><a href="/works/1/chapters/999">unrelated body link</a></div></div><div class="chapter" id="chapter-2"><div class="chapter preface"><h3 class="title"><a href="/works/1/chapters/120">Chapter 2</a></h3></div></div><div class="chapter" id="chapter-3"><div class="chapter preface"><h3 class="title"><a href="/works/1/chapters/700">Chapter 3</a></h3></div></div></div>`
	start := strings.Index(string(data), `<div id="chapters">`)
	page := string(data[:start]) + chapters + `</div></body></html>`
	m, err := ParseMetadata(AO3, strings.NewReader(page))
	if err != nil || len(m.ChapterIDs) != 3 || m.ChapterIDs[1] != "120" {
		t.Fatal(m.ChapterIDs, err)
	}
	page = strings.Replace(page, `id="chapter-2"`, `id="chapter-4"`, 1)
	m, err = ParseMetadata(AO3, strings.NewReader(page))
	if err != nil || len(m.ChapterIDs) != 0 {
		t.Fatal("partial index accepted", m.ChapterIDs, err)
	}
}
