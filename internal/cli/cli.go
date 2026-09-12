// Package cli adapts shell arguments and output to the reusable Sailune library.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"unicode"

	sailune "github.com/styxnanda/sailune-go"
	"github.com/styxnanda/sailune-go/internal/platform"
)

const help = `Sailune — local fanfiction bookmarks

Usage: sailune [--data PATH] COMMAND [OPTIONS]

Commands:
  add URL       Bookmark an AO3 or FanFiction.net work
  list          List or search bookmarks
  open ID       Open a work in the default browser (--chapter N or --next)
  resume ID     Open the next unread chapter in the default browser
  show ID       Show one bookmark
  update ID     Change personal metadata or progress
  refresh ID    Fetch the latest source metadata for a saved bookmark
  delete ID     Permanently remove one bookmark
  auth SITE     Import, inspect, or clear an AO3/FFN browser session

Session setup (optional):
  auth ao3 --login           Open default browser, sign in, then consent to import
  auth ffn --login           Sign in through the default browser
  auth ao3 --cookies-from-browser brave    Import directly from Brave
  auth ffn --cookies-from-browser firefox  Import directly from Firefox
  auth ao3 --cookies FILE    Import a Netscape cookies.txt browser export
  auth ffn --cookies FILE    Import a FanFiction.net browser export
  auth SITE                 Show local session status (does not verify login)
  auth SITE --clear         Remove saved cookies
  auth SITE --migrate-from DIR  Encrypt and move a legacy session
  --sessions DIR            Global session directory override

Add/update options:
  --title TEXT --author TEXT --status STATUS --chapter N
  --tags TAG1,TAG2 --notes TEXT
  Status: planned (default), reading, completed, hold, dropped
  Chapter: last chapter read, default 0; never inferred from a URL

Add/refresh fetch options:
  --cookies-from-browser BROWSER[:PROFILE]  Import this site's browser cookies
  --no-fetch       Bookmark offline with manual metadata (add only)
  --user-agent UA  User-Agent used for the request (or SAILUNE_USER_AGENT)
  By default, add fetches title, authors, summary, tags, and story statistics.
  --title and --author override fetched values when nonempty.

Update customization:
  --rating 0..5 --review-notes TEXT --last-read DATE --added DATE
  --summary TEXT --fandoms LIST --source-tags LIST --language TEXT
  --content-rating TEXT --words N --chapters N --total-chapters N
  --complete[=false] --published DATE --source-updated DATE
  --reset-overrides FIELD,...|all
  Custom metadata survives refresh. Dates: see update --help.

List options:
  --query TEXT --site ao3|ffn --status STATUS --tag TAG
  --author TEXT --fandom TEXT --language TEXT --source-tag TEXT
  --complete[=false] --unread --min-rating N --min-words N --max-words N
  --sort added|last-read|updated|source-updated|title|author|rating|words|progress
  --desc --limit N --offset N

Open/resume: --print-url resolves without launching; opening never marks read.

All commands accept --json. Options can precede or follow URL/ID.
Use COMMAND --help for command options. Updates replace supplied fields;
use an empty string to clear title, author, tags, or notes.

Storage: --data PATH > SAILUNE_DATA > ~/.sailune/bookmarks.json
Sessions: --sessions DIR > SAILUNE_SESSIONS > OS-local Sailune session directory
Fetch failures do not save a bookmark; use --no-fetch for an offline entry.
`

func Run(args []string, out, errOut io.Writer) error {
	return RunContext(context.Background(), args, out, errOut)
}

func RunContext(ctx context.Context, args []string, out, errOut io.Writer) error {
	return run(ctx, args, out, errOut, nil)
}

func run(ctx context.Context, args []string, out, errOut io.Writer, fetcher sailune.MetadataFetcher) error {
	return runWithLogin(ctx, args, out, errOut, fetcher, loginDependencies{input: os.Stdin, open: sailune.OpenLoginBrowser})
}

func runWithLogin(ctx context.Context, args []string, out, errOut io.Writer, fetcher sailune.MetadataFetcher, loginDeps loginDependencies) error {
	root := flag.NewFlagSet("sailune", flag.ContinueOnError)
	root.SetOutput(errOut)
	path := root.String("data", "", "library JSON path")
	sessions := root.String("sessions", "", "private session directory")
	root.Usage = func() { fmt.Fprint(out, help) }
	if err := root.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	args = root.Args()
	if len(args) == 0 || args[0] == "help" {
		_, err := fmt.Fprint(out, help)
		return err
	}
	command := args[0]
	switch command {
	case "add", "list", "show", "update", "refresh", "delete", "auth", "open", "resume":
	default:
		return fmt.Errorf("unknown command %q; run sailune --help", command)
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(errOut)
	jsonOutput := fs.Bool("json", false, "output JSON for scripts and future GUI clients")
	var title, author, status, tags, notes, query, site, tag string
	var chapter int
	var customization customizationOptions
	var listing listOptions
	var next, printURL bool
	if command == "update" {
		customization.register(fs)
	}
	if command == "list" {
		listing.register(fs)
	}
	if command == "open" || command == "resume" {
		fs.BoolVar(&printURL, "print-url", false, "print destination without launching the browser")
		if command == "open" {
			fs.BoolVar(&next, "next", false, "open the next unread chapter")
			fs.IntVar(&chapter, "chapter", 0, "open a specific chapter (1-based)")
		}
	}
	var noFetch, clearSession, login bool
	var cookies, userAgent, browserCookies, migrateFrom string
	if command == "auth" || command == "add" || command == "refresh" {
		fs.StringVar(&browserCookies, "cookies-from-browser", "", "family/browser[:profile]: chromium/brave|chrome|chromium|edge|opera|vivaldi, gecko/firefox; legacy names accepted")
	}
	if command == "auth" {
		fs.StringVar(&migrateFrom, "migrate-from", "", "encrypt and move an existing session from this directory")
		fs.BoolVar(&login, "login", false, "open the default browser and ask for consent before importing cookies")
		fs.StringVar(&cookies, "cookies", "", "Netscape cookies.txt file exported after browser login")
		fs.BoolVar(&clearSession, "clear", false, "remove the saved session for this site")
	}
	if command == "add" {
		fs.BoolVar(&noFetch, "no-fetch", false, "save manually without a network request")
	}
	if command == "add" || command == "refresh" {
		fs.StringVar(&userAgent, "user-agent", os.Getenv("SAILUNE_USER_AGENT"), "User-Agent for the fetch")
	}
	if command == "add" || command == "update" {
		fs.StringVar(&title, "title", "", "story title (manual)")
		fs.StringVar(&author, "author", "", "author (manual)")
		fs.StringVar(&status, "status", "", "planned, reading, completed, hold, dropped")
		fs.IntVar(&chapter, "chapter", 0, "last chapter read (0 = unread)")
		fs.StringVar(&tags, "tags", "", "comma-separated tags; replaces existing tags")
		fs.StringVar(&notes, "notes", "", "personal notes")
	}
	if command == "list" {
		fs.StringVar(&query, "query", "", "search title, author, URL, tags, notes")
		fs.StringVar(&site, "site", "", "ao3 or ffn")
		fs.StringVar(&status, "status", "", "filter by reading status")
		fs.StringVar(&tag, "tag", "", "exact tag (case-insensitive)")
	}
	fs.Usage = func() {
		operand := " ID"
		if command == "add" {
			operand = " URL"
		}
		if command == "list" {
			operand = ""
		}
		if command == "auth" {
			operand = " SITE"
		}
		fmt.Fprintf(errOut, "Usage: sailune [--data PATH] %s%s [OPTIONS]\n", command, operand)
		fs.PrintDefaults()
	}
	if err := parseFlags(fs, args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	expected := 1
	if command == "list" {
		expected = 0
	}
	if fs.NArg() != expected {
		return fmt.Errorf("%s expects %d argument(s); run sailune %s --help", command, expected, command)
	}
	if browserCookies != "" {
		if _, err := sailune.ParseBrowserSpec(browserCookies); err != nil {
			return err
		}
		if noFetch {
			return errors.New("--cookies-from-browser cannot be combined with --no-fetch")
		}
	}
	var id int64
	if command == "show" || command == "update" || command == "refresh" || command == "delete" || command == "open" || command == "resume" {
		var err error
		id, err = strconv.ParseInt(fs.Arg(0), 10, 64)
		if err != nil || id < 1 {
			return errors.New("ID must be a positive integer")
		}
	}
	if *path == "" {
		*path = os.Getenv("SAILUNE_DATA")
	}
	if *path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		*path = filepath.Join(home, ".sailune", "bookmarks.json")
	}
	lib := sailune.Library{Store: sailune.Store{Path: *path}}
	defaultSessions := *sessions == "" && os.Getenv("SAILUNE_SESSIONS") == ""
	if *sessions == "" {
		*sessions = os.Getenv("SAILUNE_SESSIONS")
	}
	if *sessions == "" {
		var err error
		*sessions, err = sailune.DefaultSessionDir()
		if err != nil {
			return err
		}
	}
	sessionStore := sailune.SessionStore{Dir: *sessions}
	// Surface legacy credentials instead of silently fetching as a guest after
	// the default session location changes. Offline commands never inspect them.
	if defaultSessions && migrateFrom == "" && (command == "auth" || command == "add" && !noFetch || command == "refresh") {
		var targetSite sailune.Site
		if command == "auth" {
			targetSite = sailune.Site(fs.Arg(0))
		}
		if command == "add" {
			_, targetSite, _, _ = sailune.NormalizeURL(fs.Arg(0))
		}
		if command == "refresh" {
			if b, e := lib.Get(id); e == nil {
				targetSite = b.Site
			}
		}
		if targetSite == sailune.AO3 || targetSite == sailune.FFN {
			if legacy := legacySessionDir(*path, *sessions, targetSite); legacy != "" {
				return fmt.Errorf("legacy session found in %s; run auth %s --migrate-from %q to encrypt and move it", legacy, targetSite, legacy)
			}
		}
	}
	var result any
	var err error
	switch command {
	case "auth":
		if migrateFrom != "" && (login || cookies != "" || browserCookies != "" || clearSession) {
			return errors.New("--migrate-from cannot be combined with login, import, or clear options")
		}
		if login && (cookies != "" || clearSession) {
			return errors.New("--login cannot be combined with --cookies or --clear")
		}
		if (cookies != "" && clearSession) || (browserCookies != "" && (cookies != "" || clearSession)) {
			return errors.New("use only one of --cookies, --cookies-from-browser, or --clear")
		}
		sessionSite := sailune.Site(fs.Arg(0))
		if sessionSite != sailune.AO3 && sessionSite != sailune.FFN {
			return errors.New("site must be ao3 or ffn")
		}
		if migrateFrom != "" {
			result, err = sessionStore.Migrate(sessionSite, migrateFrom)
		} else if login {
			result, err = interactiveLogin(ctx, sessionSite, browserCookies, sessionStore, errOut, loginDeps)
		} else if browserCookies != "" {
			result, err = sessionStore.ImportBrowser(ctx, sessionSite, browserCookies)
		} else if cookies != "" {
			f, openErr := os.Open(cookies)
			if openErr != nil {
				return openErr
			}
			defer f.Close()
			result, err = sessionStore.Import(sessionSite, f)
		} else {
			if clearSession {
				if err := sessionStore.Clear(sessionSite); err != nil {
					return err
				}
			}
			result, err = sessionStore.Status(sessionSite)
		}
	case "add":
		b := sailune.Bookmark{URL: fs.Arg(0), Title: title, Author: author, Status: sailune.Status(status), Chapter: chapter, Tags: strings.Split(tags, ","), Notes: notes}
		if noFetch {
			result, err = lib.Add(b)
		} else {
			if fetcher == nil {
				fetcher = &sailune.Scraper{Sessions: sessionStore, UserAgent: userAgent}
			}
			if browserCookies != "" {
				fetcher = browserSessionFetcher{source: browserCookies, sessions: sessionStore, next: fetcher}
			}
			result, err = lib.AddScraped(ctx, b, fetcher)
		}
	case "refresh":
		if fetcher == nil {
			fetcher = &sailune.Scraper{Sessions: sessionStore, UserAgent: userAgent}
		}
		if browserCookies != "" {
			fetcher = browserSessionFetcher{source: browserCookies, sessions: sessionStore, next: fetcher}
		}
		result, err = lib.Refresh(ctx, id, fetcher)
	case "list":
		f := listing.filter
		f.Query, f.Site, f.Status, f.Tag = query, sailune.Site(site), sailune.Status(status), tag
		fs.Visit(func(flag *flag.Flag) {
			if flag.Name == "complete" {
				f.Complete = &listing.complete
			}
		})
		result, err = lib.List(f)
	case "open", "resume":
		suppliedChapter := false
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "chapter" {
				suppliedChapter = true
			}
		})
		if next && suppliedChapter {
			return errors.New("use either --next or --chapter")
		}
		if suppliedChapter && chapter < 1 {
			return errors.New("chapter must be at least 1")
		}
		var destination string
		if command == "resume" || next {
			destination, err = lib.ResumeURL(id)
		} else {
			destination, err = lib.OpenURL(id, chapter)
		}
		if err != nil {
			return err
		}
		if !printURL {
			open := loginDeps.openStory
			if open == nil {
				open = platform.OpenBrowser
			}
			if err := open(ctx, destination); err != nil {
				return err
			}
		}
		result = openResult{ID: id, URL: destination, Opened: !printURL}
	case "show":
		result, err = lib.Get(id)
	case "update":
		p := sailune.Patch{}
		fs.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "title":
				p.Title = &title
			case "author":
				p.Author = &author
			case "status":
				value := sailune.Status(status)
				p.Status = &value
			case "chapter":
				p.Chapter = &chapter
			case "tags":
				value := strings.Split(tags, ",")
				p.Tags = &value
			case "notes":
				p.Notes = &notes
			}
		})
		if err := customization.patch(fs, &p); err != nil {
			return err
		}
		if p == (sailune.Patch{}) {
			return errors.New("update requires at least one metadata option")
		}
		result, err = lib.Update(id, p)
	case "delete":
		err = lib.Delete(id)
		result = struct {
			DeletedID int64 `json:"deleted_id"`
		}{id}
	}
	if err != nil {
		return err
	}
	if *jsonOutput {
		switch v := result.(type) {
		case sailune.Bookmark:
			result = outputBookmark(v)
		case []sailune.Bookmark:
			items := make([]bookmarkOutput, 0, len(v))
			for _, b := range v {
				items = append(items, outputBookmark(b))
			}
			result = items
		}
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	switch value := result.(type) {
	case openResult:
		_, err = fmt.Fprintln(out, value.URL)
	case sailune.SessionStatus:
		_, err = fmt.Fprintf(out, "Site: %s\nSession saved: %t\nUsable cookies for site root: %d\nLogin validity is checked when fetching a work.\n", value.Site, value.Configured, value.UsableCookies)
	case []sailune.Bookmark:
		if len(value) == 0 {
			_, err = fmt.Fprintln(out, "No bookmarks found.")
			return err
		}
		w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tSITE\tSTATUS\tPROGRESS\tRATING\tADDED\tLAST READ\tTITLE\tAUTHOR")
		for _, b := range value {
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", b.ID, b.Site, b.Status, progressText(b), ratingText(b.Rating), dateText(b.CreatedAt), dateText(b.LastReadAt), displayTitle(b), safe(b.Author))
		}
		return w.Flush()
	case sailune.Bookmark:
		_, err = fmt.Fprintf(out, "ID: %d\nTitle: %s\nAuthor: %s\nURL: %s\nSite: %s\nStatus: %s\nChapter: %d\nTags: %s\nNotes: %s\n", value.ID, displayTitle(value), safe(value.Author), value.URL, value.Site, value.Status, value.Chapter, safe(strings.Join(value.Tags, ", ")), safe(value.Notes))
		if err == nil {
			_, err = fmt.Fprintf(out, "Progress: %s\nAdded: %s\nLast read: %s\nPersonal rating: %s\nReview notes: %s\n", progressText(value), dateText(value.CreatedAt), dateText(value.LastReadAt), ratingText(value.Rating), safe(value.ReviewNotes))
		}
		if err == nil && (value.Metadata != nil || value.Overrides != nil) {
			m := value.EffectiveMetadata()
			_, err = fmt.Fprintf(out, "Summary: %s\nFandoms: %s\nLanguage: %s\nContent rating: %s\nWords: %d\nPublished chapters: %d\nStory complete: %t\nSource tags: %s\nPlanned chapters: %d\nPublished: %s\nSource updated: %s\n", safe(m.Summary), safe(strings.Join(m.Fandoms, ", ")), safe(m.Language), safe(m.Rating), m.Words, m.Chapters, m.Complete, safe(strings.Join(m.Tags, ", ")), m.TotalChapters, safe(m.Published), safe(m.Updated))
		}
	default:
		_, err = fmt.Fprintf(out, "Deleted bookmark %d.\n", id)
	}
	return err
}

// Import only after AddScraped has checked input and duplicates.
type browserSessionFetcher struct {
	source   string
	sessions sailune.SessionStore
	next     sailune.MetadataFetcher
}

func (f browserSessionFetcher) Fetch(ctx context.Context, raw string) (sailune.Metadata, error) {
	_, site, _, err := sailune.NormalizeURL(raw)
	if err != nil {
		return sailune.Metadata{}, err
	}
	if _, err := f.sessions.ImportBrowser(ctx, site, f.source); err != nil {
		return sailune.Metadata{}, err
	}
	return f.next.Fetch(ctx, raw)
}

func displayTitle(b sailune.Bookmark) string {
	if b.Title == "" {
		return string(b.Site) + " " + b.WorkID
	}
	return safe(b.Title)
}

// Keep user-entered terminal control characters out of human-readable output.
func safe(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}

// The standard flag parser stops at the first operand. Reorder known options
// while preserving their values, so `add URL --title Title` also works.
func parseFlags(fs *flag.FlagSet, args []string) error {
	var options, operands []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			operands = append(operands, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			operands = append(operands, a)
			continue
		}
		name := strings.TrimPrefix(strings.TrimPrefix(a, "-"), "-")
		name, _, hasValue := strings.Cut(name, "=")
		f := fs.Lookup(name)
		if f == nil {
			if name == "help" || name == "h" {
				return fs.Parse([]string{a})
			}
			return fmt.Errorf("unknown option %q", a)
		}
		options = append(options, a)
		boolean, ok := f.Value.(interface{ IsBoolFlag() bool })
		if !hasValue && !(ok && boolean.IsBoolFlag()) {
			i++
			if i == len(args) {
				return fmt.Errorf("option %s requires a value", a)
			}
			options = append(options, args[i])
		}
	}
	return fs.Parse(append(append(options, "--"), operands...))
}

func legacySessionDir(dataPath, destination string, site sailune.Site) string {
	target, err := filepath.Abs(filepath.Join(destination, string(site)+".json"))
	if err != nil {
		return ""
	}
	if _, err := os.Stat(target); err == nil {
		return ""
	}
	roots := []string{filepath.Join(filepath.Dir(dataPath), "sessions")}
	// Explicit session overrides are useful for isolated libraries and tests.
	if os.Getenv("SAILUNE_SESSIONS") == "" {
		if home, err := os.UserHomeDir(); err == nil {
			roots = append(roots, filepath.Join(home, ".sailune/sessions"))
		}
	}
	for _, root := range roots {
		source, err := filepath.Abs(filepath.Join(root, string(site)+".json"))
		if err == nil && source != target {
			if info, err := os.Stat(source); err == nil && info.Mode().IsRegular() {
				return root
			}
		}
	}
	return ""
}

type openResult struct {
	ID     int64  `json:"id"`
	URL    string `json:"url"`
	Opened bool   `json:"opened"`
}
