# Authenticating to AO3 and FanFiction.net

Sailune can fetch public works without signing in. To request works using your
account, sign in through your browser and import a cookie export. Sailune does
not ask for your password or automate login forms, MFA, or CAPTCHA.

## Prepare a cookie export

Use a browser cookie exporter you trust that supports **Netscape cookies.txt**.
Export cookies for the current site, including HTTP-only cookies. A JSON export,
a screenshot of the browser's cookie table, or a copied `Cookie:` request header
will not work.

A Netscape cookie file has seven tab-separated fields per cookie:

```text
domain  include_subdomains  path  secure  expires  name  value
```

The displayed headings are explanatory; they are not a line to add to the file.
An exporter supplies the actual tab separators and cookie attributes. See
[curl's format documentation](https://curl.se/docs/http-cookies.html) for the
specification. Do not change cookie domains or expiry dates to make an import
succeed.

Save exports outside your repository, for example in a private directory in your
home folder. Cookies can grant access to your account. Do not paste them into
issues, chat messages, screenshots, or command-line cookie arguments. On macOS
or Linux, restrict an exported file's permissions with:

```sh
chmod 600 /path/to/ao3.cookies.txt
```

## AO3

1. Open [archiveofourown.org](https://archiveofourown.org/) in your browser and
   sign in using its **Log In** control.
2. Open a work you want to bookmark. For a restricted work, confirm that its
   content is visible while signed in.
3. While on that site, export its cookies in Netscape format to a private file
   such as `ao3.cookies.txt`. Include HTTP-only cookies; do not select only the
   cookies visible to page JavaScript.
4. Import the export:

   ```sh
   sailune auth ao3 --cookies /path/to/ao3.cookies.txt
   ```

5. Add the actual work URL to test access through Sailune:

   ```sh
   sailune add 'https://archiveofourown.org/works/WORK_ID'
   ```

Replace `WORK_ID` with the work's numeric ID. AO3 setup uses cookies scoped to
`archiveofourown.org` or `www.archiveofourown.org`. An export from an alternate
AO3 hostname is not imported; use the canonical site for this setup.

## FanFiction.net

1. Open [www.fanfiction.net](https://www.fanfiction.net/) in your browser and
   sign in through the site's login flow.
2. Open the story on `www.fanfiction.net`, complete any browser challenge, and
   confirm the story is visible.
3. Export the site's cookies in Netscape format to a private file such as
   `ffn.cookies.txt`, including HTTP-only cookies.
4. Import the export:

   ```sh
   sailune auth ffn --cookies /path/to/ffn.cookies.txt
   ```

5. Add the actual story URL:

   ```sh
   sailune add 'https://www.fanfiction.net/s/STORY_ID/1'
   ```

Replace `STORY_ID` with the numeric story ID. Sailune fetches FFN's desktop site,
including when given a mobile URL. Export from a logged-in desktop-site visit:
host-only cookies for `m.fanfiction.net` cannot authenticate `www.fanfiction.net`.
Domain-wide cookies for `.fanfiction.net` retain their original scope.

## Inspect, renew, and remove sessions

```sh
sailune auth ao3
sailune auth ffn --json
```

`configured` means a session file exists. `usable_cookies` counts unexpired
cookies matching the site's HTTPS root. It does not count only login cookies,
and it does not prove that the server still accepts the login. Adding a work is
the access check; use a URL that is not already bookmarked because duplicates
are rejected before fetching.

When a session expires, sign in again in your browser, export fresh cookies, and
repeat the corresponding `auth ... --cookies` command. A valid import replaces
only that site's saved session. Invalid imports leave the existing session
untouched. Cookies for unrelated sites and expired cookies are discarded.

Remove the saved session with:

```sh
sailune auth ao3 --clear
sailune auth ffn --clear
```

Clearing Sailune's copy does not log out your browser or revoke the server's
session. Saved cookies are reused across commands, with server cookie updates
and deletions persisted automatically. Imported session cookies with expiry `0`
remain saved until cleared or invalidated by the server.

The importer copies cookies into its session store. The original export is no
longer needed after a successful import and can be removed from your private
export directory.

## Session location

By default, sessions are stored in `~/.sailune/sessions/`, separately from the
bookmark library. To use another directory, pass the same global option to both
setup and subsequent commands:

```sh
sailune --sessions /private/path/sessions auth ao3 --cookies /path/to/ao3.cookies.txt
sailune --sessions /private/path/sessions add 'https://archiveofourown.org/works/WORK_ID'
```

Alternatively set `SAILUNE_SESSIONS`. Precedence is `--sessions`, then
`SAILUNE_SESSIONS`, then `sessions/` beside the library file. A custom `--data`
path changes that default unless you explicitly set a session directory.

Session files use private permissions on POSIX systems but are not encrypted.
Cookie values are excluded from command output and bookmark exports.

## Troubleshooting

| Result | Action |
| --- | --- |
| Invalid Netscape format | Export as cookies.txt with seven tab-separated fields, not JSON or a raw header. |
| No unexpired cookies for this site | Check the selected site, domain, and export format; sign in again and export fresh cookies. |
| Login required or session expired | Confirm browser access, then re-import a fresh export for that site. |
| Site denied access or browser challenge | Open the work in the browser, complete the challenge, and export fresh cookies. |
| Rate limit reached | Wait before retrying; Sailune does not retry automatically. |
| Session is busy | Wait for another command to finish. Remove a stale lock only after verifying no Sailune process is running. |
| Metadata not found | Confirm the work is accessible. Report a sanitized reproduction if its page layout has changed. |

Some browser clearance cookies depend on the User-Agent. If needed, obtain your
browser's User-Agent from its developer tools by evaluating the read-only
expression `navigator.userAgent`, then pass that exact value:

```sh
sailune add 'https://www.fanfiction.net/s/STORY_ID/1' --user-agent 'YOUR_BROWSER_USER_AGENT'
```

For repeated use, set `SAILUNE_USER_AGENT`. Matching cookies and User-Agent is
not guaranteed to satisfy a browser-bound challenge. Sailune does not bypass
CAPTCHA or other browser checks. You can still save an offline bookmark:

```sh
sailune add 'https://www.fanfiction.net/s/STORY_ID/1' --no-fetch \
  --title 'Story title' --author 'Author name'
```

Failed fetches leave the bookmark library unchanged. For AO3, requests include
`view_adult=true` to acknowledge the adult-content interstitial.
