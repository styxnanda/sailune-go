# Sailune

A local-first fanfiction bookmarking CLI written in Go. Save AO3 and
FanFiction.net stories, organize your reading list, and track progress.
No account, server, third-party dependencies, or network access is required.

## Build

Requires Go 1.22 or later:

```sh
go build -o bin/sailune ./cmd/sailune
./bin/sailune --help
```

Use `go run ./cmd/sailune` in place of `sailune` below, or install the executable
into your Go bin directory with `go install ./cmd/sailune`.

## Quick start

```sh
sailune add 'https://archiveofourown.org/works/123456' \
  --title 'A favorite story' --author 'SomeAuthor' --tags 'fantasy,to-read'
sailune add 'https://www.fanfiction.net/s/123456/4/Story-Title' \
  --title 'Another story' --status reading --chapter 4
sailune list
sailune list --site ao3 --status planned
sailune list --query 'favorite' --tag fantasy
sailune show 1
sailune update 1 --status reading --chapter 3 --notes 'Continue this weekend'
sailune update 1 --tags 'fantasy,favorites'
sailune update 1 --notes ''
sailune list --json > bookmarks-export.json
sailune delete 2
```

Options may precede or follow the URL/ID. Quote URLs and text containing shell
special characters. Use `sailune COMMAND --help` for command options.

## MVP behavior

- `add`, `list`, `show`, `update`, and `delete`; all support `--json`.
- AO3 work/chapter URLs and FFN desktop/mobile story URLs normalize to a
  canonical work URL. Chapter, query, and fragment variations cannot create
  duplicate bookmarks. Profiles, AO3 series, and other sites are rejected.
- Titles and authors are **manual**. Without a title, the CLI displays the site
  and work ID. The MVP does not fetch metadata or verify that a story exists.
- Status is `planned` (default), `reading`, `completed`, `hold`, or `dropped`.
  It describes your reading state, not the author's publication state.
- `--chapter` is the last chapter read, starting at `0` for unread. Neither the
  input URL nor the reading status automatically changes it.
- Updates replace only supplied fields. `--tags` replaces all tags; empty text
  clears a text field or tags. Tags are trimmed and deduplicated ignoring case.
- Search matches title, author, URL, notes, and tags case-insensitively. Filters
  combine with AND; `--tag` matches a whole tag. Lists use creation order.
- IDs are stable and never reused after deletion. Deletion is immediate.
- JSON output is one bookmark for add/show/update, an array for list (including
  `[]` for no matches), or `{"deleted_id":2}` for delete. Errors go to stderr
  with a nonzero exit status. Timestamps are UTC RFC 3339.

## Storage and backups

Default: `~/.sailune/bookmarks.json`. Override with `SAILUNE_DATA` or a global
option **before** the command. Precedence: `--data` > `SAILUNE_DATA` > default.

```sh
sailune --data ./my-library.json add 'https://archiveofourown.org/works/123456'
SAILUNE_DATA=./my-library.json sailune list --json
```

The versioned JSON file is written to a private temporary file, synced, then
atomically replaced. A lock file prevents concurrent writers from overwriting
each other; a competing write fails and can be retried. Readers see a complete
snapshot. Use a local filesystem; network shares and cloud-sync conflicts are
outside this MVP. Files are private to the user but are not encrypted.

After a crash, a stale `bookmarks.json.lock` may remain. Verify that no Sailune
writer is running before removing it. Invalid or unsupported library files cause
an error and are left untouched. To back up or restore, copy the library file
while no writer is running. `list --json` exports records, not the storage
envelope; there is no import command yet.

## Development and architecture

```sh
go test ./...
go test -race ./...
go vet ./...
```

The root `sailune` Go package exposes `Library`, `Bookmark`, `Filter`, `Patch`,
`Store`, and `NormalizeURL`, independently of flags and terminal output. A Go GUI
can call this package directly; other frontends can invoke CLI JSON mode.
`internal/cli` handles arguments and output; `cmd/sailune` is the entry point.

```go
lib := sailune.Library{Store: sailune.Store{Path: "bookmarks.json"}}
bookmark, err := lib.Add(sailune.Bookmark{
    URL: "https://archiveofourown.org/works/123456",
    Title: "My reading list entry",
})
```

This replaces the Java/Maven scaffolding and its unused checksum command.
There is no database migration: the previous repository did not implement
bookmark persistence.

## Roadmap

- Optional AO3/FFN metadata adapters with handling for restricted and unavailable pages.
- Import, richer filtering, and story update checks.
- Custom-site adapters and a GUI using the core library.

Downloading, login/cookies, scraping, synchronization, and custom sites are
outside this first MVP.
