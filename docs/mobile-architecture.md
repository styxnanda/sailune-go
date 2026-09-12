# Mobile architecture decision

Recommendation: build a dedicated mobile GUI that reuses Sailune's library
behavior through an embedded Go library and a small mobile bridge. Do not duplicate
the bookmark rules in another implementation or treat the desktop executable as
a subprocess backend on iOS. This is a future milestone, not mobile support shipped
today.

## Why

Go supports generating language bindings and building libraries for Android and
iOS using [gomobile bind](https://pkg.go.dev/golang.org/x/mobile/cmd/gomobile) and
[gobind](https://pkg.go.dev/golang.org/x/mobile/cmd/gobind). This makes code reuse
possible without a terminal or CLI process. Supported binding types are limited,
so the current public Go API is not claimed to bind unchanged. A narrow bridge
will be needed for its structs, pointers, contexts, collections, and errors.

Apple's [App Review Guidelines, 2.5.2](https://developer.apple.com/app-store/review/guidelines/#software-requirements)
require self-contained apps and container-scoped data access. A desktop pattern
that shells out to browser launch commands and reads another browser's profile is
not a suitable iOS architecture. Embedding the Go logic is different from running
the desktop CLI. Android terminal deployments can be useful experiments, but do
not establish an iOS-compatible consumer app design.

## Shared behavior already available

The CLI adapts arguments and output to Library methods. Today, the shared library
owns customization, ratings, reading progress, filtering/sorting, storage, and
OpenURL/ResumeURL destination selection. Bookmark.EffectiveMetadata and
Bookmark.ReadingProgress provide presentation-ready values without parsing terminal
output. The CLI's additive JSON output exposes those values too.

Browser launching belongs to the desktop platform adapter; resolving a destination
has no browser or login side effects. Opening a story does not mark it read.
The mobile app should invoke its platform URL-opening API on the returned URL.

## Future work, intentionally deferred

- Build a small binding-safe facade and prove iOS/Android builds on real devices.
  Audit dependencies and separate desktop authentication/keyring code at the
  mobile build boundary; the current root package still exposes desktop auth.
- Use app-container storage and mobile secure storage, with explicit adapters.
- Design site-specific login through supported mobile web flows and app-owned
  sessions. Do not assume Safari/Chrome cookie files can be imported or that a
  system-browser login automatically shares cookies with the app's HTTP client.
  AO3/FFN support and browser challenges need separate validation.
- Decide on synchronization, conflict handling, and background refresh within
  mobile lifecycle limits. JSON snapshot export/import transfers libraries but is not a mobile sync protocol.

Today's changes do not alter login or implement a mobile runtime, server, sync,
or mobile browser/session handling.
