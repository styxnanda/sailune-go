# Sailune

## About

Sailune is a fanfiction bookmarking CLI written in Go for Archive of Our Own
(AO3) and FanFiction.net (FFN). It fetches story metadata, keeps a local reading
list, and tracks personal reading progress. Optional browser-cookie sessions
allow requests using your logged-in account.

Features include automatic titles, authors, summaries, fandoms, source tags,
and story statistics; search and filters; personal tags and notes; and JSON
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

### Authenticate to AO3 and FFN

Login is optional. Sign in through your browser, export the site's cookies in
**Netscape cookies.txt format**, and import them:

```sh
sailune auth ao3 --cookies /path/to/ao3.cookies.txt
sailune auth ffn --cookies /path/to/ffn.cookies.txt
```

Subsequent `add` commands automatically use that site's saved session. Follow the
[authentication guide](docs/authentication.md) for separate AO3 and FFN setup
steps, cookie export requirements, session renewal, and troubleshooting.

```sh
sailune auth ao3           # Inspect local session configuration
sailune auth ffn --json
sailune auth ao3 --clear   # Remove a saved session
```

Session status reports local cookies, not verified login validity. Cookies are
credentials: keep exports and session files private. Browser challenges may
still prevent fetching even with an imported session.

### Options and output

| Option | Behavior |
| --- | --- |
| `--status` | `planned` (default), `reading`, `completed`, `hold`, or `dropped` |
| `--chapter` | Last chapter read; defaults to `0` |
| `--tags` | Comma-separated personal tags; replaces the existing list on update |
| `--notes` | Personal notes; an empty string clears them |
| `--query` | Case-insensitive search across personal and source metadata |
| `--site` | Filter a list by `ao3` or `ffn` |
| `--tag` | Filter a list by an exact personal tag, ignoring case |
| `--json` | Machine-readable output; supported by every command |
| `--no-fetch` | Save an offline bookmark with `add` |
| `--user-agent` | Set the request User-Agent for `add` |

Reading status and chapter progress describe your reading, independently of the
story's publication state. Source metadata is saved separately from personal
tags and notes. Updates change only supplied personal fields and do not refresh
source metadata. Tags are trimmed and deduplicated ignoring case. Filters combine
with AND; lists use creation order. IDs remain stable and are never reused.
Deletion is immediate.

JSON output is a bookmark for add/show/update, an array for list (including `[]`
for no matches), `{"deleted_id":2}` for delete, or session status for auth.
Errors go to stderr and return a nonzero exit status.

Command options can precede or follow a URL/ID. Global `--data` and `--sessions`
options must come before the command. Use `sailune COMMAND --help` for details.

### Storage and backups

| Setting | Precedence, highest first |
| --- | --- |
| Library file | `--data PATH`, `SAILUNE_DATA`, `~/.sailune/bookmarks.json` |
| Session directory | `--sessions DIR`, `SAILUNE_SESSIONS`, `sessions/` beside the library |
| Request User-Agent | `add --user-agent VALUE`, `SAILUNE_USER_AGENT`, Sailune's default |

The default session directory is `~/.sailune/sessions/`. Changing the library path
also changes the default session directory unless one is explicitly supplied.

```sh
sailune --data ./my-library.json --sessions /private/path/sessions list
```

Use a local filesystem. Writes atomically replace the JSON files, and locks
prevent simultaneous writers from overwriting one another. If a library or
session is busy, retry after the other command finishes. After a crash, verify
that no Sailune process is running before removing a stale `.lock` file.

Back up or restore the library by copying its file while no writer is running.
`list --json` exports records rather than the storage envelope; it is not a
restorable library backup. There is no import command. Keep session credentials
separate from bookmark backups; session files are not encrypted.

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

### Validate changes

```sh
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
```

Tests use synthetic HTML fixtures and injected HTTP transports, so no live
account is needed. Add regression tests for parsing or session-handling changes.
Keep test fixtures free of real cookies, account details, and copyrighted story
text. Never commit cookie exports or session files.

Open an issue for bugs or propose a pull request with a description of the
change and the checks run. Include a sanitized reproduction for site parsing
problems.

## Credits

- [Go](https://go.dev/) and [golang.org/x/net](https://pkg.go.dev/golang.org/x/net)
  provide the runtime, HTML parser, and public suffix data.
- [AO3 / Organization for Transformative Works](https://github.com/otwcode/otwarchive)
  provides the open-source work templates used as a parser reference.
- [FanFicFare](https://github.com/JimmXinu/FanFicFare) provides a reference for
  FanFiction.net metadata structure.
- [curl's cookie documentation](https://curl.se/docs/http-cookies.html) describes
  the Netscape cookie format used for session imports.

Sailune is an independent project and is not affiliated with AO3 or FanFiction.net.

## License

Licensed under the GNU General Public License, version 3. See [LICENSE](LICENSE).
