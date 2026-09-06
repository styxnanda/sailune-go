package sailune

import "testing"

func TestNormalizeURL(t *testing.T) {
	for _, tc := range []struct {
		input, want string
		site        Site
		id          string
	}{
		{"https://archiveofourown.org/works/123", "https://archiveofourown.org/works/123", AO3, "123"},
		{" http://www.archiveofourown.org/works/00123/chapters/456?view_adult=true#comments ", "https://archiveofourown.org/works/123", AO3, "123"},
		{"https://m.fanfiction.net/s/123/8/Story-Title?x=1", "https://www.fanfiction.net/s/123/1", FFN, "123"},
		{"https://FANFICTION.NET/s/123/", "https://www.fanfiction.net/s/123/1", FFN, "123"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			url, site, id, err := NormalizeURL(tc.input)
			if err != nil || url != tc.want || site != tc.site || id != tc.id {
				t.Fatalf("got %q %q %q %v", url, site, id, err)
			}
		})
	}
	for _, input := range []string{
		"", "not a URL", "ftp://archiveofourown.org/works/1", "https://example.com/works/1",
		"https://archiveofourown.org.evil.test/works/1", "https://archiveofourown.org/series/123",
		"https://archiveofourown.org/works/0", "https://archiveofourown.org/works/1/other",
		"https://user@archiveofourown.org/works/1", "https://archiveofourown.org:443/works/1",
		"https://fanfiction.net/u/123", "https://fanfiction.net/s/12/0/Title",
		"https://fanfiction.net/s/123/1/title/extra", "https://archiveofourown.org/works/%31",
	} {
		if _, _, _, err := NormalizeURL(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}
