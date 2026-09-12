# Sailune

## About

Sailune is a fanfiction bookmarking CLI written in Go for Archive of Our Own
(AO3) and FanFiction.net (FFN). It fetches story metadata, keeps a local reading
list, and tracks personal reading progress. Optional browser-cookie sessions
allow requests using your logged-in account.

Features include automatic titles, authors, summaries, fandoms, source tags,
and story statistics; search and filters; personal tags and notes; personal ratings and reviews; browser resume; and JSON
output. Sailune stores metadata, not chapter text.

## Usage

### Installation

Install [Go](https://go.dev/doc/install) 1.25 or later, then run:

```sh
go install github.com/styxnanda/sailune-go/cmd/sailune@latest
sailune --help
```

The executable is installed in `GOBIN`, or `$(go env GOPATH)/bin` when `GOBIN`
is unset. Add that directory to your shell's `PATH` if necessary.

### Add and organize bookmarks

Replace the example story URLs with works you want to bookmark:

```sh
sailune add 'https://archiveofourown.org/works/123456'
sailune add 'https://www.fanfiction.net/s/123456/1/Story-Title' --tags 'to-read'
sailune list
sailune list --site ao3 --status planned
sailune list --query 'friendship' --tag favorites
sailune show 1
sailune update 1 --status reading --chapter 3 --notes 'Continue this weekend'
sailune update 1 --tags 'fantasy,favorites'
sailune update 1 --notes ''
sailune list --json > bookmarks-export.json
sailune delete 2
```

`add` fetches metadata automatically. AO3 chapter links and FFN mobile/desktop
chapter links resolve to one bookmark per work. A duplicate is rejected before
fetching, and a failed fetch does not save a bookmark. `list` and `show` work
offline.

Use nonempty `--title` and `--author` values to override fetched display values.
For manual or offline bookmarking:

```sh
sailune add 'https://archiveofourown.org/works/123456' --no-fetch \
  --title 'My favorite story' --author 'SomeAuthor'
```

### Refresh source metadata

```sh
sailune refresh 1
sailune refresh 1 --json
sailune refresh 1 --cookies-from-browser chromium/brave
```

Use the bookmark ID from `sailune list`. `refresh` fetches the latest source
metadata, including chapter and word counts, summary, completion status, and
source dates. It uses the saved AO3 or FFN session, and accepts `--user-agent`.
Your display title, author, reading status, last chapter read, tags, and notes
stay unchanged. The latest source title and authors are in the JSON `metadata`
object. Offline bookmarks can also be refreshed. A failed fetch preserves the
previous bookmark and metadata. `update` continues to edit personal fields only.

### Open and resume reading

```sh
sailune open 1                  # Open the work in the default browser
sailune resume 1                # Open the next unread chapter
sailune open 1 --next           # Same as resume
sailune open 1 --chapter 3      # Open a specific chapter, including rereads
sailune resume 1 --print-url    # Resolve only; no browser launch
```

These commands work from saved data and never mark a chapter read or import
cookies. The browser uses its own signed-in session. Desktop launching supports
macOS, Windows, and Linux. `--json` still launches unless `--print-url` is supplied.

FFN chapter destinations use chapter numbers. AO3 uses chapter IDs captured from
the source chapter selector or entire-work chapter headings on add/refresh; refresh older bookmarks once to save
the index. If the index is unavailable, Sailune asks you to refresh or open the
work manually. It never guesses AO3 chapter IDs. If progress is already at or
past the saved published count, resume reports that you are caught up; refresh
for new chapters or use open to reread. Saved source counts/indexes can be stale.

### Progress, dates, personal ratings, and customization

```sh
sailune update 1 --chapter 3 --status reading
sailune update 1 --rating 5 --review-notes 'Excellent pacing and characterization'
sailune update 1 --notes 'Remember to recommend this to a friend'
sailune update 1 --last-read '2026-09-10T21:30:00+07:00' --added 2026-01-15
sailune update 1 --summary 'My own summary' --fandoms 'Example Fandom'
sailune update 1 --words 42000 --chapters 12 --total-chapters 20 --complete=false
sailune update 1 --language English --content-rating Teen --source-tags 'Magic,AU'
sailune update 1 --published 2025-12-01 --source-updated 2026-09-01
sailune update 1 --reset-overrides summary,words
sailune update 1 --rating 0 --review-notes ''
```

`list` and `show` display read/published progress, percent, unread chapters, date
added, last read, and personal stars. Progress uses published chapters, not the
planned total. Unknown counts show `N/?`; percentages cap at 100% without changing
your saved progress. Personal reading status remains explicitly controlled and
independent of publication completion.

Recording a positive `--chapter` sets last read to now, including recording the
same chapter again. `--chapter 0` clears last read. An explicit `--last-read`
overrides that behavior, accepts RFC3339, YYYY-MM-DD (midnight UTC), `now`, or an
empty string to clear. `--added` accepts the same nonempty date formats. Other
edits, refreshes, and opening the browser leave last read unchanged. Older
libraries remain readable and show an unknown last-read date until you record
one. Human dates use the local timezone; JSON timestamps use UTC.

Personal `--rating` is an integer from 1 to 5; 0 means unrated. `--review-notes`
is independent of `--notes` and the source's `--content-rating`. All can be edited
individually; empty text clears a field.

Custom summary, fandoms, source tags, language, content rating, word/chapter
counts, completion, and source dates are stored in `overrides`, separate from
raw `metadata`. Display, progress, search, and filters use effective values.
Refresh updates raw metadata and preserves every override. Empty text/list,
zero counts, and `--complete=false` are explicit overrides. Reset named fields
with `--reset-overrides`, or use `all` to follow the source again. Reset happens
before overrides supplied in the same command. Titles/authors continue to use
the existing individually editable display fields; refresh preserves them.

Identity (ID, canonical URL, site, work ID), fetched chapter IDs, and automatic
update/fetch timestamps are managed by Sailune, not user-editable metadata.
Personal customization flags are available on `update`; use add then update
for a fully manual bookmark.

### Search, filter, and sort

```sh
sailune list --query 'magic friendship' --fandom 'Example Fandom'
sailune list --status reading --unread --sort last-read --desc
sailune list --complete --min-words 10000 --max-words 100000
sailune list --author 'Writer' --language English --source-tag AU
sailune list --min-rating 4 --sort rating --desc --limit 20 --offset 0
```

Search matches every whitespace-separated term, ignoring case, across display
fields, notes, reviews, personal tags, and effective source metadata. Terms may
match different fields. `--author` is a substring filter; fandom, language,
source tag, and personal tag filters are exact, ignoring case. All filters
combine with AND. `--complete=false` selects ongoing stories with known
publication state, excluding manual entries without that information.
`--unread` requires a known published count greater than your reading progress.

Sort keys: `added` (default), `last-read`, `updated` (any bookmark edit or refresh),
`source-updated`, `title`, `author`, `rating`, `words`, and `progress` (percent).
Sorts ascend unless `--desc` is supplied; ties use ascending ID. Unknown dates,
unrated stars, and unknown numeric values sort as empty/zero. `--limit 0` means
unlimited; offset and limit apply after filtering and sorting.

### Authenticate to AO3 and FFN

Login is optional. Open your default browser and sign in manually:

```sh
sailune auth ao3 --login
sailune auth ffn --login
```

Return to the terminal after signing in. Sailune asks which browser/profile you
used, then asks you to type `yes` before it imports and saves that site's cookies.
Press Enter without answering, type `cancel`, or press Ctrl+C to stop. No cookies
are imported until you confirm. No extension is needed.

To preselect the browser/profile for that prompt:

```sh
sailune auth ao3 --login --cookies-from-browser brave:Default
```

If already signed in, you can explicitly import without the interactive flow:

```sh
sailune auth ao3 --cookies-from-browser brave
sailune auth ffn --cookies-from-browser firefox
```

Or import and bookmark in one command:

```sh
sailune add 'https://archiveofourown.org/works/61993375' --cookies-from-browser brave
sailune add 'https://www.fanfiction.net/s/123456/1' --cookies-from-browser firefox
```

Browser sources are grouped into `chromium` and `gecko` families:

- Chromium: `chromium/brave`, `chromium/chrome`, `chromium/chromium`,
  `chromium/edge`, `chromium/opera`, `chromium/vivaldi`.
- Gecko: `gecko/firefox` (or `gecko`). Compatible Firefox forks can use
  `gecko:/absolute/profile/path`; automatic discovery searches Firefox only.

Add `:PROFILE`, for example `chromium/brave:Default` or
`"chromium/chrome:Profile 1"`. Browser names are still needed to find each
installation's profile and encryption key. Existing names such as `brave` and
`firefox` remain valid. Bare `chromium` selects the Chromium application for
compatibility. If several profiles are found, specify one.
On macOS, Chromium imports may prompt for access to the browser's Safe Storage
item in Keychain. See the [authentication guide](docs/authentication.md) for
profile discovery, OS support, session renewal, and troubleshooting.

Netscape cookie-file imports remain available:

```sh
sailune auth ao3 --cookies /path/to/ao3.cookies.txt
sailune auth ffn --cookies /path/to/ffn.cookies.txt
sailune auth ao3           # Inspect local session configuration
sailune auth ao3 --clear   # Remove a saved session
```

Subsequent adds reuse the saved session. Only the requested site's cookies are
imported. Session status reports local cookies, not verified login validity.
Keep session files private. Browser challenges may still prevent fetching.

### Options and output

| Option | Behavior |
| --- | --- |
| `--status` | `planned` (default), `reading`, `completed`, `hold`, or `dropped` |
| `--chapter` | Last chapter read; defaults to `0` |
| `--tags` | Comma-separated personal tags; replaces the existing list on update |
| `--notes` | Personal notes; an empty string clears them |
| `--query` | Case-insensitive search; every whitespace-separated term must occur in personal or effective source metadata |
| `--site` | Filter a list by `ao3` or `ffn` |
| `--tag` | Filter a list by an exact personal tag, ignoring case |
| `--json` | Machine-readable output; supported by every command |
| `--cookies-from-browser` | Import the selected browser profile on `auth` or `add` |
| `--no-fetch` | Save an offline bookmark with `add` |
| `--user-agent` | Set the request User-Agent for `add` |

Reading status and chapter progress describe your reading, independently of the
story's publication state. Source metadata is saved separately from personal
tags and notes. Updates change only supplied fields and do not fetch source metadata.
Custom metadata overrides remain separate from the fetched snapshot. Tags are trimmed and deduplicated ignoring case. Filters combine
with AND; lists sort by date added ascending unless another sort is requested. IDs remain stable and are never reused.
Deletion is immediate.

JSON output is a bookmark for add/show/update/refresh, an array for list (including `[]`
for no matches), `{"deleted_id":2}` for delete, or session status for auth.
Bookmark JSON also includes `effective_metadata` and computed `progress`; the library
file stores only source/personal data, not these derived output fields.
Open/resume JSON contains `id`, `url`, and `opened`; use `--print-url` to avoid launching.
Errors go to stderr and return a nonzero exit status.

Command options can precede or follow a URL/ID. Global `--data` and `--sessions`
options must come before the command. Use `sailune COMMAND --help` for details.

### Storage and backups

| Setting | Precedence, highest first |
| --- | --- |
| Library file | `--data PATH`, `SAILUNE_DATA`, `~/.sailune/bookmarks.json` |
| Session directory | `--sessions DIR`, `SAILUNE_SESSIONS`, OS-local Sailune directory |
| Request User-Agent | `add --user-agent VALUE`, `SAILUNE_USER_AGENT`, Sailune's default |

Sessions are encrypted with AES-256-GCM; keys stay in the OS credential store.
The default session directory is independent of `--data`:

- macOS: `~/Library/Application Support/Sailune/sessions/`
- Windows: `%LOCALAPPDATA%\Sailune\sessions\`
- Linux: `$XDG_STATE_HOME/sailune/sessions/`, or `~/.local/state/sailune/sessions/`

Keep session directories local. Put only your bookmark library in a cloud-sync,
SMB, or NAS location. Copying an encrypted session file alone does not transfer
its credential-store key to another computer. See the
[authentication guide](docs/authentication.md#session-location-and-encryption)
for platform requirements and migration of old plaintext sessions.

```sh
sailune --data ./my-library.json --sessions /private/path/sessions list
```

Library writes atomically replace the JSON file, with a lock to coordinate
writers on filesystems that support exclusive creation and atomic rename.
Cloud-sync clients do not coordinate these locks across devices. Use one writer
at a time, let synchronization finish before switching devices, and keep backups.
SMB/NAS behavior depends on the filesystem and server; simultaneous distributed
editing and automatic conflict merging are not supported. If a library or
session is busy, retry after the other command finishes. After a crash, verify
that no Sailune process is running before removing a stale `.lock` file.

Back up or restore the library by copying its file while no writer is running.
`list --json` exports records rather than the storage envelope; it is not a
restorable library backup. There is no import command. Keep session credentials
separate from bookmark backups, even though session files are encrypted.

See [the mobile architecture decision](docs/mobile-architecture.md) for the planned
shared-library approach to a future mobile app.

## Contributing

### Set up the project

Install Go 1.25 or later and Git, then clone and build:

```sh
git clone https://github.com/styxnanda/sailune-go.git
cd sailune-go
go mod download
go build -o bin/sailune ./cmd/sailune
./bin/sailune --help
```

You can also run commands with `go run ./cmd/sailune`. Use `--data` and
`--sessions` pointing to temporary paths when testing against a separate library.

### Project layout

```text
cmd/sailune/             CLI executable entry point
internal/cli/            Commands, output, and interactive login prompts
internal/model/          Bookmark types, validation, and site URL rules
internal/library/        Bookmark operations and JSON storage
internal/auth/           Sessions, browser cookies, encryption, and browser launch
internal/scrape/         HTTP fetching and site metadata parsers
internal/scrape/testdata/ Synthetic HTML fixtures
tests/                  Public Go API compatibility tests
docs/                   Authentication guide
sailune.go              Public Go API
```

Tests live beside the implementation they exercise. External Go callers import
`github.com/styxnanda/sailune-go`; implementation packages remain internal.

### Validate changes

```sh
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
```

Tests use synthetic HTML, temporary browser databases, and injected HTTP
transports, so no live account or Keychain access is needed. Add regression tests for parsing or session-handling changes.
Keep test fixtures free of real cookies, account details, and copyrighted story
text. Never commit cookie exports or session files.

Open an issue for bugs or propose a pull request with a description of the
change and the checks run. Include a sanitized reproduction for site parsing
problems.

## Credits

- [Go](https://go.dev/) and [golang.org/x/net](https://pkg.go.dev/golang.org/x/net)
  provide the runtime, HTML parser, and public suffix data.
- [go-keyring](https://github.com/zalando/go-keyring) provides OS credential-store
  integration for session encryption keys.
- [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) provides the pure-Go
  SQLite reader for browser profiles.
- [yt-dlp](https://github.com/yt-dlp/yt-dlp) provides browser storage and cookie
  encryption format references.
- [AO3 / Organization for Transformative Works](https://github.com/otwcode/otwarchive)
  provides the open-source work templates used as a parser reference.
- [FanFicFare](https://github.com/JimmXinu/FanFicFare) provides a reference for
  FanFiction.net metadata structure.
- [curl's cookie documentation](https://curl.se/docs/http-cookies.html) describes
  the Netscape cookie format used for session imports.

Sailune is an independent project and is not affiliated with AO3 or FanFiction.net.

## License

Licensed under the GNU General Public License, version 3. See [LICENSE](LICENSE).
