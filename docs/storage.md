# SQLite library design

## Storage and query behavior

The existing modernc.org/sqlite dependency is used through database/sql. The
library keeps its public Go API, with additive Export, ExportFile, Import,
ImportFile, Transfer, ImportResult, and DefaultLibraryPath APIs.

SQLite user_version=1 and application_id identify the schema. Bookmarks have a
canonical URL uniqueness constraint, a primary-key ID, the complete JSON record,
and materialized effective fields for filtering/sorting. Facets provide indexed
personal-tag, source-tag, and fandom lookups. Materialized fields are regenerated
in the same transaction on add, update, refresh, and import. Unicode exact matching
preserves Go EqualFold behavior; substring searches use lowercased text and instr,
so percent/underscore and SQL-looking input remain literal. Sort columns come
only from an allowlist; all user query values use bound SQL parameters.

Get and duplicate checks use keys. List filters and pages within SQLite, decoding
only returned rows. Search remains substring-based, not FTS. Scalar indexes add
storage/write overhead in exchange for sorted pagination without decoding all
bookmarks. Payloads and searchable fields are both plaintext.

Each operation opens/closes its own connection. Writes use immediate transactions,
a five-second busy timeout, foreign keys, FULL synchronous mode, and secure_delete.
Rollback journaling avoids a persistent WAL sidecar; it does not make live cloud
synchronization safe. Refresh fetches outside the write transaction and applies
source metadata to the latest bookmark so concurrent personal edits survive.
IDs are allocated transactionally and never reused. Export uses a deferred read
transaction for a consistent snapshot, including the next-ID watermark.

Initialization builds and closes a temporary database, then publishes it with an
exclusive hard link, preserving any existing destination. Export similarly
publishes a complete, fsynced JSON snapshot without overwrite. These paths require
local filesystem hard-link support. A cloud client should upload the finished
export, not the temporary file or a live SQLite database. Network filesystems are
unsupported as live library locations.

## Migration and transfer guarantees

Migration is a pristine-destination import of a validated legacy JSON envelope.
The source is not deleted, renamed, or modified. Import validates the complete
input before starting a transaction; invalid versions, identities, duplicates,
ratings, progress, overrides, and unknown fields are rejected. A runtime error
rolls back every insertion. An empty schema may remain after a failed transaction;
existing bookmark data remains unchanged.

Restore preserves IDs and next_id, including IDs belonging to previously deleted
bookmarks. A library that previously allocated IDs is not pristine even if empty.
Merge skips canonical-URL duplicates without overwriting them and assigns local
IDs to new records. It preserves dates and personal fields but does not reconcile
updates/deletions. Export/import does not copy authentication data. JSON imports
are bounded to 256 MiB; export streams records and is not subject to this limit.
For larger libraries, use multiple smaller merge envelopes or a future streaming
import; a >256 MiB export is not currently importable as one file.

## Security boundary

The default location is OS-local app-data, with private file/directory permissions.
Windows protected file ACLs grant the current account and SYSTEM access. Custom
existing parent folders are not modified; choose a dedicated private folder,
especially on Windows where journal files inherit directory permissions.
The database and exports are plaintext. Same-user processes and administrators
can access them. Security through obfuscation is deliberately not claimed.
Full-disk encryption protects a powered-off device; it does not protect a signed-in
account from itself. Session encryption remains unchanged and separate.

A future encrypted database would need a vetted SQLite encryption implementation,
platform builds, key lifecycle/recovery, and explicit portable backup semantics.
Encrypting every record while retaining plaintext search indexes would leak the
indexed information; decrypting the full library per query would undo this
migration's scalability improvement. Neither shortcut is used.

## Validation

Tests cover full-field JSON round trips, legacy migration, ID watermarks, merge
idempotence, invalid inputs, SQL parameterization, Unicode facet matching,
permissions, alias/overwrite refusal, transaction rollback, writer contention,
concurrent adds, source-refresh concurrency, and indexed pagination query plans.
Existing command and public API tests continue to exercise the shared library.

Run synthetic benchmarks with:

```sh
go test ./internal/library -run '^$' -bench SQLite -benchmem
```

They measure page reads, individual reads, substring search, and updates against
10,000 bookmarks, including connection overhead. Setup is outside measured loops.
Measured on an Apple M4 with this synthetic workload: 20-row page ~0.26 ms,
individual lookup ~0.16 ms, update ~0.64 ms, and no-match substring scan ~4.7 ms.
Results include connection overhead and reflect a local/cache-warmed workload.
They are not a comparison against the old backend. Results depend on hardware,
filesystem, cache state, and workload; they are not cross-platform guarantees.

References:
- [SQLite network caveats](https://www.sqlite.org/useovernet.html)
- [SQLite backup snapshots](https://www.sqlite.org/backup.html)
- [SQLite secure-delete pragma](https://www.sqlite.org/pragma.html#pragma_secure_delete)
