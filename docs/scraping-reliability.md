# Scraping investigation — 2026-09-19

The 23 user-supplied stories were fetched as a guest from this development
machine with the actual public Go `Scraper.Fetch` API. Success requires parsed
metadata, including title and author; an HTTP 200 alone does not count. No
personal library or stored login cookies were accessed. Requests were sequential
with a one-second pause between stories. Measurements are small snapshots of
changing upstream conditions, not an SLA or a controlled causal experiment.

| Run | AO3 | FFN | Overall |
| --- | --- | --- | --- |
| Original single attempt | 11/15 | 2/8 | 13/23 (56.5%) |
| Bounded retries, 30s budget | 13/15 | 2/8 | 15/23 (65.2%) |
| Bounded retries, Android 15s budget | 14/15 | 0/8 | 14/23 (60.9%) |

AO3 work 83882181 required login in both runs. Of the 14 remaining AO3 links,
13 succeeded after the change (92.9%). Work 92521346 still returned 525 after
four attempts in the second run; the three original AO3 transient failures
(92136251, 92508311, 92683621) recovered. FFN responses varied between valid
pages, Cloudflare browser challenges, and a page without work metadata. Neither
retrying a challenge nor inventing partial metadata qualifies as success.

[Cloudflare documents 525](https://developers.cloudflare.com/support/troubleshooting/http-status-codes/cloudflare-5xx-errors/error-525/)
as an SSL handshake failure between Cloudflare and its origin. Direct curl
requests also reproduced 525. Both HTTP/1.1 and HTTP/2 encountered it, so changing
protocols is not an established fix. Tests with original FFN slugs and its mobile
host also encountered challenges; they are not reliable fallback endpoints.

The 15-second follow-up recovered all 14 guest-accessible AO3 works, with the
same login-restricted work remaining inaccessible. All eight FFN requests were
challenged in that run. This variability is why no aggregate 90% claim is made.

## Recovery algorithm

1. Normalize/validate the work URL; use the existing scoped session cookie jar.
2. Fetch over verified HTTPS with the existing redirect and 8 MiB response limits.
3. Retry only transient status/transport failures, at most four attempts total.
   Wait roughly 0.6, 1.2 and 2.4 seconds plus up to 250 ms jitter; honor longer
   `Retry-After` values. Keep an 8s attempt ceiling and the caller's total budget.
4. Stop immediately for cancellation, forbidden/challenge/login responses,
   unavailable works, invalid content, or a server delay exceeding the budget.
5. Parse and validate metadata before storing anything. Preserve the library
   when fetching fails. Keep personal overrides separate from source metadata.

## What would be needed for 90% across both sites

The measured overall result does **not** reach 90%. For restricted AO3 works,
CLI/Desktop users need an authorized AO3 session through the existing login or
cookie-import flow. Cookie import does not guarantee that a browser-bound FFN
challenge will accept Go's HTTP client. Android currently fetches as a guest.

The next architectural step for challenge-heavy sites is an explicit, user-led
browser handoff: load the exact story in a real browser/WebView, let the user
complete login/challenges, then return validated work-page metadata through an
origin-checked bridge. This requires separate desktop and Android integration,
including session storage and lifecycle/security tests. It is not implemented
or counted as a success in this change. Never auto-solve challenges, harvest
unrelated browser cookies, or substitute cached/search snippets as current data.

## Experimental silent-browser prototype

FFN may now use an optional browser loader after a challenge, missing metadata,
or the 5-second HTTP budget expires. The existing 30-second total bound remains
for CLI/Desktop and 15 seconds for Android. The browser returns header HTML and
its final URL; the core verifies HTTPS, FFN origin, the requested story identity,
a 1 MiB payload cap, and valid metadata before any save. No challenge is clicked
or solved automatically. Login requirements, 404 and 429 do not trigger fallback.

Recovery is serialized and unsuccessful attempts receive a 60-second cooldown
within the running client. Cancellation stops browser work and rejects late
callbacks. User cancellation does not cause cooldown. CLI/Desktop launch an
installed Chrome/Chromium headlessly on demand with a dedicated profile beneath
the session directory, never a personal browser profile. They close the process
after each attempt. If Chrome is absent, recovery fails quietly. Android supplies
its own on-demand, unattached WebView with app-private cookies and no native
JavaScript bridge; prompts/permissions are denied and it is destroyed when done.

The first eight-link desktop run yielded 2/8 overall, with blocked browser
attempts exhausting the deadline. A separate direct browser probe also timed
out. Therefore the prototype has **not demonstrated improved FFN reliability**.
This exploratory run preceded the correction to apply cooldown after browser
timeouts, so sequential blocked attempts in the final version stop earlier.
A real Chromium fixture test passed for JavaScript-rendered header extraction;
that is integration evidence, not evidence of passing FFN's live challenges.

Reproduce the optional headless path with an isolated profile:

```sh
go run scripts/check-scraping.go -timeout 15s \
  -browser-profile /absolute/path/to/isolated-profile URL [URL ...]
SAILUNE_BROWSER_TEST=1 go test ./browser
```

The Android emulator's direct WebView test also returned **0/8 metadata pages**:
all eight attempts ended at approximately 15.02 seconds. This test exercised the
browser adapter alone (not HTTP plus fallback) so it isolates the proposed
recovery mechanism. The three deterministic Android instrumentation checks
passed: JavaScript header extraction without an attached window, blocked-page
timeout without a prompt, and cancellation destroying the WebView. The real
network test passed its lifecycle/deadline assertions but did not meet the
metadata success objective. A physical-device run could differ; no device
success is assumed. These results do not justify claiming that an invisible
browser solves FFN access restrictions.
