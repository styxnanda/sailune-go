// Package cli adapts shell arguments and output to the reusable Sailune library.
package cli

import (
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
)

const help = `Sailune — local fanfiction bookmarks

Usage: sailune [--data PATH] COMMAND [OPTIONS]

Commands:
  add URL       Bookmark an AO3 or FanFiction.net work
  list          List or search bookmarks
  show ID       Show one bookmark
  update ID     Change personal metadata or progress
  delete ID     Permanently remove one bookmark

Add/update options:
  --title TEXT --author TEXT --status STATUS --chapter N
  --tags TAG1,TAG2 --notes TEXT
  Status: planned (default), reading, completed, hold, dropped
  Chapter: last chapter read, default 0; never inferred from a URL

List options:
  --query TEXT --site ao3|ffn --status STATUS --tag TAG

All commands accept --json. Options can precede or follow URL/ID.
Use COMMAND --help for command options. Updates replace supplied fields;
use an empty string to clear title, author, tags, or notes.

Storage: --data PATH > SAILUNE_DATA > ~/.sailune/bookmarks.json
No network requests are made; titles and authors are entered manually.
`

func Run(args []string, out, errOut io.Writer) error {
	root := flag.NewFlagSet("sailune", flag.ContinueOnError)
	root.SetOutput(errOut)
	path := root.String("data", "", "library JSON path")
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
	case "add", "list", "show", "update", "delete":
	default:
		return fmt.Errorf("unknown command %q; run sailune --help", command)
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(errOut)
	jsonOutput := fs.Bool("json", false, "output JSON for scripts and future GUI clients")
	var title, author, status, tags, notes, query, site, tag string
	var chapter int
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
	var id int64
	if command == "show" || command == "update" || command == "delete" {
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
	var result any
	var err error
	switch command {
	case "add":
		result, err = lib.Add(sailune.Bookmark{URL: fs.Arg(0), Title: title, Author: author, Status: sailune.Status(status), Chapter: chapter, Tags: strings.Split(tags, ","), Notes: notes})
	case "list":
		result, err = lib.List(sailune.Filter{Query: query, Site: sailune.Site(site), Status: sailune.Status(status), Tag: tag})
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
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	switch value := result.(type) {
	case []sailune.Bookmark:
		if len(value) == 0 {
			_, err = fmt.Fprintln(out, "No bookmarks found.")
			return err
		}
		w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tSITE\tSTATUS\tCHAPTER\tTITLE\tAUTHOR")
		for _, b := range value {
			fmt.Fprintf(w, "%d\t%s\t%s\t%d\t%s\t%s\n", b.ID, b.Site, b.Status, b.Chapter, displayTitle(b), safe(b.Author))
		}
		return w.Flush()
	case sailune.Bookmark:
		_, err = fmt.Fprintf(out, "ID: %d\nTitle: %s\nAuthor: %s\nURL: %s\nSite: %s\nStatus: %s\nChapter: %d\nTags: %s\nNotes: %s\n", value.ID, displayTitle(value), safe(value.Author), value.URL, value.Site, value.Status, value.Chapter, safe(strings.Join(value.Tags, ", ")), safe(value.Notes))
	default:
		_, err = fmt.Fprintf(out, "Deleted bookmark %d.\n", id)
	}
	return err
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
