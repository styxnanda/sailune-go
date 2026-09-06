package sailune

import (
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// ParseMetadata extracts work headers only, never chapter text or HTML markup.
// This is separately callable so adapters can be tested without live accounts.
func ParseMetadata(site Site, r io.Reader) (Metadata, error) {
	if _, err := siteHost(site); err != nil {
		return Metadata{}, err
	}
	doc, err := html.Parse(r)
	if err != nil {
		return Metadata{}, err
	}
	title := strings.ToLower(nodeText(first(doc, element("title"))))
	if strings.Contains(title, "just a moment") || strings.Contains(title, "attention required") || first(doc, func(n *html.Node) bool {
		return attr(n, "id") == "challenge-form" || attr(n, "id") == "cf-challenge-running"
	}) != nil {
		return Metadata{}, ErrChallenge
	}
	m := Metadata{Authors: []string{}, Fandoms: []string{}, Tags: []string{}}
	if site == AO3 {
		m = parseAO3(doc, m)
	} else {
		m = parseFFN(doc, m)
	}
	if m.Title == "" || len(m.Authors) == 0 {
		// Header login forms exist on normal guest pages, so classify gates only
		// when there was no valid work header.
		if strings.Contains(title, "log in") || strings.Contains(title, "login") || first(doc, func(n *html.Node) bool { return n.Data == "form" && strings.Contains(attr(n, "action"), "login") }) != nil {
			return Metadata{}, ErrLoginRequired
		}
		text := strings.ToLower(nodeText(doc))
		if strings.Contains(text, "story not found") || strings.Contains(text, "unable to locate story") || strings.Contains(text, "mystery work") {
			return Metadata{}, ErrWorkUnavailable
		}
		return Metadata{}, ErrMetadata
	}
	return m, nil
}

func parseAO3(doc *html.Node, m Metadata) Metadata {
	work := first(doc, func(n *html.Node) bool { return attr(n, "id") == "workskin" })
	preface := first(work, hasClass("preface"))
	m.Title = nodeText(first(preface, func(n *html.Node) bool { return n.Data == "h2" && hasClass("title")(n) }))
	byline := first(preface, hasClass("byline"))
	for _, n := range all(byline, func(n *html.Node) bool { return n.Data == "a" && strings.Contains(" "+attr(n, "rel")+" ", " author ") }) {
		if text := nodeText(n); text != "" {
			m.Authors = append(m.Authors, text)
		}
	}
	if len(m.Authors) == 0 && nodeText(byline) == "Anonymous" {
		m.Authors = []string{"Anonymous"}
	}
	summary := first(preface, hasClass("summary"))
	m.Summary = nodeText(first(summary, element("blockquote")))
	dd := func(class string) *html.Node {
		return first(doc, func(n *html.Node) bool { return n.Data == "dd" && hasClass(class)(n) })
	}
	m.Language, m.Rating = nodeText(dd("language")), nodeText(dd("rating"))
	m.Words = number(nodeText(dd("words")))
	chapters := strings.Split(nodeText(dd("chapters")), "/")
	if len(chapters) > 0 {
		m.Chapters = number(chapters[0])
	}
	if len(chapters) > 1 {
		m.TotalChapters = number(chapters[1])
		m.Complete = m.TotalChapters > 0 && m.Chapters == m.TotalChapters
	}
	m.Published, m.Updated = nodeText(dd("published")), nodeText(dd("status"))
	for _, n := range all(dd("fandom"), hasClass("tag")) {
		m.Fandoms = append(m.Fandoms, nodeText(n))
	}
	for _, class := range []string{"warning", "category", "relationship", "character", "freeform"} {
		for _, n := range all(dd(class), hasClass("tag")) {
			m.Tags = append(m.Tags, nodeText(n))
		}
	}
	m.Authors, m.Fandoms, m.Tags = cleanTags(m.Authors), cleanTags(m.Fandoms), cleanTags(m.Tags)
	return m
}

var ffnCount = regexp.MustCompile(`(?:^| - )(Chapters|Words): ([0-9,]+)`)

func parseFFN(doc *html.Node, m Metadata) Metadata {
	profile := first(doc, func(n *html.Node) bool { return attr(n, "id") == "profile_top" })
	m.Title = nodeText(first(profile, func(n *html.Node) bool { return n.Data == "b" && hasClass("xcontrast_txt")(n) }))
	for _, n := range all(profile, func(n *html.Node) bool { return n.Data == "a" && strings.HasPrefix(attr(n, "href"), "/u/") }) {
		m.Authors = append(m.Authors, nodeText(n))
	}
	m.Summary = nodeText(first(profile, func(n *html.Node) bool { return n.Data == "div" && hasClass("xcontrast_txt")(n) }))
	statsNode := first(profile, func(n *html.Node) bool {
		return n.Data == "span" && hasClass("xgray")(n) && strings.HasPrefix(nodeText(n), "Rated:")
	})
	stats := nodeText(statsNode)
	parts := strings.Split(stats, " - ")
	if len(parts) > 0 {
		m.Rating = strings.TrimPrefix(parts[0], "Rated: ")
	}
	if len(parts) > 1 {
		m.Language = parts[1]
	}
	if len(parts) > 2 {
		genres := strings.Split(strings.ReplaceAll(parts[2], "Hurt/Comfort", "Hurt-Comfort"), "/")
		known := "|Adventure|Angst|Crime|Drama|Family|Fantasy|Friendship|General|Horror|Humor|Hurt-Comfort|Mystery|Parody|Poetry|Romance|Sci-Fi|Spiritual|Supernatural|Suspense|Tragedy|Western|"
		valid := true
		for _, genre := range genres {
			if !strings.Contains(known, "|"+genre+"|") {
				valid = false
			}
		}
		if valid {
			for _, genre := range genres {
				m.Tags = append(m.Tags, strings.ReplaceAll(genre, "Hurt-Comfort", "Hurt/Comfort"))
			}
		}
	}
	m.Chapters = 1 // FFN omits Chapters for one-shots.
	for _, match := range ffnCount.FindAllStringSubmatch(stats, -1) {
		if match[1] == "Words" {
			m.Words = number(match[2])
		} else {
			m.Chapters = number(match[2])
		}
	}
	for _, part := range parts {
		if part == "Status: Complete" {
			m.Complete = true
			m.TotalChapters = m.Chapters
		}
	}
	for _, n := range all(statsNode, func(n *html.Node) bool { return attr(n, "data-xutime") != "" }) {
		seconds, err := strconv.ParseInt(attr(n, "data-xutime"), 10, 64)
		if err != nil || seconds <= 0 {
			continue
		}
		label := ""
		if n.PrevSibling != nil {
			label = nodeText(n.PrevSibling)
		}
		date := time.Unix(seconds, 0).UTC().Format("2006-01-02")
		if strings.Contains(label, "Published:") {
			m.Published = date
		}
		if strings.Contains(label, "Updated:") {
			m.Updated = date
		}
	}
	links := first(doc, func(n *html.Node) bool { return attr(n, "id") == "pre_story_links" })
	for _, n := range all(links, element("a")) {
		// Category links have only one path component; fandoms have two or more.
		path := strings.Trim(attr(n, "href"), "/")
		if strings.Contains(path, "/") {
			m.Fandoms = append(m.Fandoms, nodeText(n))
		}
	}
	m.Authors, m.Fandoms = cleanTags(m.Authors), cleanTags(m.Fandoms)
	return m
}

func number(s string) int {
	n, err := strconv.Atoi(strings.ReplaceAll(strings.TrimSpace(s), ",", ""))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func attr(n *html.Node, key string) string {
	if n != nil {
		for _, a := range n.Attr {
			if a.Key == key {
				return a.Val
			}
		}
	}
	return ""
}

func element(tag string) func(*html.Node) bool {
	return func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == tag }
}
func hasClass(class string) func(*html.Node) bool {
	return func(n *html.Node) bool {
		return strings.Contains(" "+strings.Join(strings.Fields(attr(n, "class")), " ")+" ", " "+class+" ")
	}
}

func first(n *html.Node, match func(*html.Node) bool) *html.Node {
	if n == nil {
		return nil
	}
	if match(n) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := first(c, match); found != nil {
			return found
		}
	}
	return nil
}

func all(n *html.Node, match func(*html.Node) bool) []*html.Node {
	var result []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n == nil {
			return
		}
		if match(n) {
			result = append(result, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return result
}

func nodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n == nil || (n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style")) {
			return
		}
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		if n.Data == "br" || n.Data == "p" || n.Data == "div" {
			b.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Data == "p" || n.Data == "div" {
			b.WriteByte(' ')
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}
