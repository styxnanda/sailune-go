package library

import (
	"errors"
	"fmt"
	"math"
	"regexp"

	"github.com/styxnanda/sailune-go/internal/model"
)

var chapterID = regexp.MustCompile(`^[1-9][0-9]*$`)

// OpenURL resolves a browser destination without launching a browser or marking
// anything read. Chapter zero opens the work; positive values are 1-based.
// Frontends own browser handling. AO3 chapter IDs come only from a source index.
func (l Library) OpenURL(id int64, chapter int) (string, error) {
	if chapter < 0 {
		return "", errors.New("chapter must be zero or greater")
	}
	b, err := l.Get(id)
	if err != nil {
		return "", err
	}
	return bookmarkURL(b, chapter)
}

func bookmarkURL(b Bookmark, chapter int) (string, error) {
	canonical, _, _, err := model.NormalizeURL(b.URL)
	if err != nil {
		return "", err
	}
	if chapter == 0 {
		return canonical, nil
	}
	m := b.EffectiveMetadata()
	if m.Chapters > 0 && chapter > m.Chapters {
		return "", errors.New("chapter exceeds the saved published count; refresh the bookmark first")
	}
	if b.Site == FFN {
		return fmt.Sprintf("https://www.fanfiction.net/s/%s/%d", b.WorkID, chapter), nil
	}
	if b.Metadata != nil && chapter <= len(b.Metadata.ChapterIDs) {
		cid := b.Metadata.ChapterIDs[chapter-1]
		if !chapterID.MatchString(cid) {
			return "", errors.New("invalid saved AO3 chapter ID; refresh the bookmark")
		}
		return canonical + "/chapters/" + cid, nil
	}
	if chapter == 1 {
		return canonical, nil
	}
	return "", errors.New("AO3 chapter index is unavailable; run refresh ID, or open ID to choose a chapter in the browser")
}

// ResumeURL opens the next unread chapter according to explicitly saved progress.
func (l Library) ResumeURL(id int64) (string, error) {
	b, err := l.Get(id)
	if err != nil {
		return "", err
	}
	if b.Chapter == math.MaxInt {
		return "", errors.New("chapter progress is too large")
	}
	if n := b.EffectiveMetadata().Chapters; n > 0 && b.Chapter >= n {
		return "", errors.New("caught up with saved chapters; refresh for updates or use open ID to reread")
	}
	return bookmarkURL(b, b.Chapter+1)
}
