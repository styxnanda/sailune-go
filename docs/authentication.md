# Authenticating to AO3 and FanFiction.net

Sailune can fetch public works without signing in. To request works using your
account, sign in through your browser and import cookies directly from its
profile or from a cookie export. Sailune does
not ask for your password or automate login forms, MFA, or CAPTCHA.

## Open the default browser and sign in

```sh
sailune auth ao3 --login
sailune auth ffn --login
```

1. Sailune opens the selected site's homepage in your OS default browser.
2. Use the site's login controls and complete login yourself. Sailune does not
   fill passwords, monitor the browser, or detect login completion.
3. Return to the terminal and choose `chromium` or `gecko`. Then select the
   browser and optional profile, such as `brave:Default` or
   `firefox:/absolute/profile/path`. A full source such as
   `chromium/brave:Default` can also be entered at the first prompt.
4. Review the prompt describing which site's cookies will be read and where
   they will be saved. Type `yes` to consent to the import. This replaces any
   saved session for that site.

Entering nothing, answering anything other than `yes` at the confirmation,
closing stdin, or pressing Ctrl+C cancels without importing cookies. The existing
saved session is left unchanged. Import errors also leave it unchanged.

If you know the browser/profile already, supply it to skip the selection prompt:

```sh
sailune auth ao3 --login --cookies-from-browser brave:Default
```

This still opens the OS default browser, so the selected cookie source must
match the browser/profile used to sign in. It still requires confirmation.
The default browser must be one supported by the cookie importer; a Safari
session cannot be imported. `--login` cannot be combined with `--cookies` or
`--clear`.

No credentials are read merely by opening the browser. After consent, Chromium
imports may additionally prompt for OS Keychain access. Session status describes
local cookies; adding an accessible work is the actual test of server access.
With `--json`, interactive instructions are written to stderr and the final
session status is written to stdout.

## Import directly from Brave, Chromium, or Firefox

No browser extension is needed. Sign in to AO3 or FFN in the browser profile
that will be imported, then confirm the work is readable there.

```sh
# Save sessions once; subsequent adds reuse them.
sailune auth ao3 --cookies-from-browser brave
sailune auth ffn --cookies-from-browser firefox

# Or refresh browser cookies as part of adding a work.
sailune add 'https://archiveofourown.org/works/61993375' --cookies-from-browser brave
sailune add 'https://www.fanfiction.net/s/STORY_ID/1' --cookies-from-browser firefox
```

Replace `STORY_ID` with the actual FFN story ID. Both browser families work with
both sites; `brave` and `firefox` in these examples are interchangeable. FFN is
fetched on `www.fanfiction.net`, so sign in on that desktop hostname.

The importer reads browser cookie databases using read-only SQL transactions,
including committed WAL data. It selects only the target site's rows before
decryption. It does not change browser cookies. If the database is locked, close
the browser and retry. Cookies held only in browser memory, such as some private
browsing sessions, are unavailable; use a regular profile.

### Select the browser profile

The format is `FAMILY/BROWSER[:PROFILE]`:

- `chromium/brave`, `chromium/chrome`, `chromium/chromium`, `chromium/edge`,
  `chromium/opera`, `chromium/vivaldi` share the Chromium importer.
- `gecko/firefox` shares the Firefox importer. `gecko` is a shorthand; use
  `gecko:/absolute/profile/path` for a compatible Firefox fork. Only Firefox's
  standard directories are discovered automatically; fork schemas can differ.

The browser identifies its profile location and, for Chromium, encryption key.
Old `BROWSER[:PROFILE]` values remain accepted. Bare `chromium` still means the
Chromium application; it does not automatically select Brave or Chrome.

```sh
sailune auth ao3 --cookies-from-browser chromium/brave:Default
sailune auth ffn --cookies-from-browser "chromium/chrome:Profile 1"
sailune auth ao3 --cookies-from-browser "firefox:/absolute/path/to/profile"
```

Sailune searches standard browser profile directories. It selects a profile only
when exactly one cookie database is found; otherwise it asks for a profile.
For a nonstandard installation, give an absolute profile directory, not the
cookie database filename or the browser's top-level user-data directory.

Find the profile directory in `brave://version` (Brave), `chrome://version`
(Chrome/Chromium), `edge://version` (Edge), or `about:support` (Firefox).
The latter lists a **Profile Directory/Profile Folder**. Firefox profile names
in the flag refer to the directory name, such as `abcd.default-release`, not a
friendly display name.

### Encryption and platform support

| Browser/platform | Supported formats and requirements |
| --- | --- |
| Firefox on macOS, Linux, Windows | Regular-profile SQLite cookies; seconds and newer millisecond expiry formats |
| Chromium family on macOS | `v10` AES-CBC using the browser's Safe Storage entry in macOS Keychain |
| Chromium family on Linux | `v10` basic storage; `v11` using Secret Service via the `secret-tool` executable |
| Chromium family on Windows | Legacy DPAPI and `v10` AES-GCM using the same Windows user's DPAPI key |

On macOS, a Keychain prompt may appear when importing Chromium cookies. Allow
access to the selected browser's Safe Storage item if you want to proceed. The
key is captured in memory and is not printed or saved by Sailune. Run under the
same OS account that owns the browser profile.

On Linux, install the distribution's `secret-tool` utility and unlock the login
keyring for Secret Service-backed cookies. KWallet-backed keys are not supported.
Windows application-bound `v20` encryption is also unsupported and returns a
specific error. Use Firefox or a Netscape export in those cases; do not disable
browser security or run Sailune as administrator to bypass encryption.

Partitioned cookies and Firefox container cookies are excluded to avoid merging
separate login contexts. Use the browser's regular, non-container session.

`auth --cookies-from-browser` replaces the selected site's saved session after a
successful read. `add --cookies-from-browser` performs the same import before
fetching; input validation and duplicate checks happen first. Failed imports
preserve the previous session. Cookies are never printed. Do not combine this
flag with `--cookies`, `--clear`, or `--no-fetch`.

## Alternative: prepare a cookie export

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

## AO3 with a cookie file

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

## FanFiction.net with a cookie file

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
and it does not prove that the server still accepts the login. `add URL` or
`refresh ID` tests access to a work. Success on a public work does not prove
that restricted works are accessible.

When a session expires, sign in again in your browser and rerun
`auth SITE --cookies-from-browser BROWSER`. For cookie-file imports, export
fresh cookies and repeat `auth SITE --cookies FILE`. A valid import replaces
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

## Session location and encryption

Session files use AES-256-GCM authenticated encryption with a random 256-bit key
per session file and a fresh random nonce on every save. Authentication binds
the encrypted data to the site and key ID. Cookie values, names, domains, paths,
and expiry dates are encrypted. Chunking or scrambling is not a substitute for
encryption. Files and temporary files use private permissions on POSIX systems;
plaintext cookies are not written to temporary files.

Keys are kept separately in macOS Keychain, Windows Credential Manager with
local-machine persistence, or the Linux login collection of a Secret Service
provider. Linux requires a running, unlocked Secret Service; a headless system
without one cannot save authenticated sessions. There is no plaintext fallback
when the credential store is locked, unavailable, or access is denied. Guest
fetches and offline bookmark commands do not need a credential-store key.

Default locations are independent of the bookmark file:

| Platform | Encrypted session directory |
| --- | --- |
| macOS | `~/Library/Application Support/Sailune/sessions/` |
| Windows | `%LOCALAPPDATA%\Sailune\sessions\` |
| Linux | `$XDG_STATE_HOME/sailune/sessions/`, or `~/.local/state/sailune/sessions/` |

Override with `--sessions DIR` or `SAILUNE_SESSIONS`, in that order. Keep this
directory local; Sailune cannot reliably detect every cloud-sync client or
network mount. `--data` can point to a synced bookmark file without moving
sessions. Bookmark files and exports contain no cookies or encryption keys.
Copying the session file alone to another device will not transfer its key;
authenticate separately on each device. Full OS backups, credential-store
exports, and software running as your OS account require their own protection.
Encryption does not protect against malware controlling the unlocked account.

### Migrate existing plaintext sessions

For the old default location:

```sh
sailune auth ao3 --migrate-from "$HOME/.sailune/sessions"
sailune auth ffn --migrate-from "$HOME/.sailune/sessions"
```

Run only for sites you configured. If you previously used a custom data folder,
use its old `sessions` directory instead. Migration validates the old session,
writes and verifies the encrypted destination, then removes the original file.
It refuses to overwrite a different destination session. To encrypt in place
in an explicitly chosen local directory, use the same directory for `--sessions`
and `--migrate-from`. Regular fetching refuses legacy plaintext files until they
are migrated or cleared. Encryption failures leave the source untouched.

Migration cannot remove old copies from cloud version history, backups, or other
devices. If credentials were synced, remove those copies and use the site's
session-revocation controls where available. Clearing Sailune's local session
alone does not revoke a browser/server session.

### Password prompts and renewal

Opening a browser and signing in does not grant Sailune permission to decrypt
the browser's cookie database. Chromium imports may still request Keychain or
credential-store approval. Sailune does not change browser access controls,
cache the browser's master key on disk, or suppress required OS prompts.
[Apple documents how Keychain access permission works](https://support.apple.com/guide/mac-help/allow-apps-to-access-your-keychain-kychn002/mac).

Import once, then use `add` and `refresh` without `--cookies-from-browser`.
These commands reuse Sailune's encrypted session and do not read the browser's
master key again. Unlocking Sailune's own credential-store key may still require
OS interaction. `list`, `show`, and personal `update` remain offline.

Cookies with `Expires` or `Max-Age` stop being used when they expire. Session
cookies without an expiry are retained, but the site can invalidate them at any
time. Real `add`/`refresh` requests automatically persist any replacement cookies
or deletions issued by the server. Sailune never extends expiry timestamps
locally and does not send background keep-alive requests merely because you use
the CLI. Session expiry and renewal remain server-controlled, as described by
[OWASP's session management guidance](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html).
A status/health check cannot renew a revoked session; sign in and import again
when the site requires it.

## Troubleshooting

| Result | Action |
| --- | --- |
| Multiple browser profiles | Use `BROWSER:PROFILE` or an absolute profile directory. |
| Browser database missing or locked | Confirm the profile path; close the browser and retry if locked. |
| Cannot decrypt browser cookies | Use the same OS account and unlock Keychain/Secret Service; check the supported-format table. |
| Application-bound v20 cookies | Use Firefox or a Netscape export. |
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

### Diagnose FFN access

A successful `auth` command means cookies were saved locally. It does not prove
that FFN accepted the login or that a story can be fetched. Test with the story
URL using `sailune add 'https://www.fanfiction.net/s/STORY_ID/1'`.

If the website itself will not sign in, cookie import cannot complete that login.
If importing fails, check the selected profile and the reported database or key
error. If adding the story fails, report the exact error and story URL without
cookie values. Responses with Cloudflare's `cf-mitigated: challenge` header are
identified as challenges, including when returned with HTTP 503. This follows
[Cloudflare's documented challenge detection](https://developers.cloudflare.com/cloudflare-challenges/challenge-types/challenge-pages/detect-response/).
Reimporting cookies is not proof that a challenge has been resolved.
