---
PLAN: "feat: Manifest, Store.Ensure/Read/Prune — large artifacts downloaded once, verified and kept in OPFS"
TAG: v0.1.0
EXECUTOR: jules
REVIEWER: none
---

> This plan is dispatched via the CodeJob workflow. See skill: agents-workflow.
>
> Dependencies, all published: `webtyp.com/device` v0.1.0, `webtyp.com/files` v0.0.4 (`Store`,
> `Remover`), `webtyp.com/opfs` v0.1.2, `webtyp.com/weights` v0.3.0 (tests only). This repo is a fresh
> `gonew`: `artifacts.go` holds a placeholder — **delete it** (the API below replaces it).

# Plan — `webtyp/artifacts` v0.1.0

Master plan: [PWA_ARTIFACTS_MASTER_PLAN.md](https://github.com/webtyp/app/blob/main/docs/PWA_ARTIFACTS_MASTER_PLAN.md),
decisions D-PWA-6, D-PWA-7, D-PWA-8, D-PWA-10. **Read `AGENTS.md` first** (browser library, TinyGo
decides, forbidden imports).

## Why

A webtyp application with an in-browser model downloads 400–850 MB of weights. That must happen
**once**, with progress, resumable after a closed tab, verified against a known SHA-256, stored in
OPFS (not IndexedDB, not Cache Storage: both hand back the whole file and double the memory), and
old versions must be deleted after the new one is verified. `weights.Load` used to do part of this
in IndexedDB; it was deleted (weights v0.3.0). This library is now the only way to keep an artifact.

## Design gate

1. **Prior art.**
   - **WebLLM**: each model has an id with its version; its shards are cached by id; a new version is
     another id; deleting the old one is an explicit call (`deleteModelAllInfoInCache`). We take: id +
     version, explicit pruning.
   - **transformers.js**: caches by URL in Cache Storage; resuming is not supported; a 700 MB file is
     re-downloaded from zero after a closed tab. We do not follow it (Cache Storage + no resume).
   - **Ollama `pull`**: blobs named by their SHA-256 digest, downloaded in parts, resumed, verified
     before use. We take: verify the digest before the artifact is usable; resume from what is on disk.
   - **HTTP Range** (RFC 9110 §14): resuming is `Range: bytes=<from>-<to>` answered with `206`. The
     webtyp server serves static files with `http.ServeContent`, which supports it.
2. **Novice-name test.** `artifacts.ParseManifest(data)`, `m.Find("decider-0.8b")`,
   `store := artifacts.New(fs)`, `store.Has(a)`, `store.Ensure(a, profile, progress)`,
   `store.Read(a)`, `store.Prune(keep)`. Each reads as a sentence.
3. **Complexity ledger.** New library: +3 types (`Artifact`, `Manifest`, `Store`), 6 methods/functions.
   Ways to keep an artifact: 1 (was 2 with `weights.Load`, already deleted).
4. **Where it belongs.** Its own repo: run time, browser, consumed by the agent Worker and by
   `agentlab`. It depends on contracts only: `files.Store` (any implementation; `opfs` in the
   browser, `files/mem` in tests), `device.Requirement`/`device.Profile`, `webtyp.com/fetch`.
   It never imports `opfs` (the composition root passes `opfs.Open("<module>")`, D-PWA-10).
5. **What it deletes.** Nothing (new capability; its predecessor was deleted in `weights` v0.3.0).

## The API

```go
package artifacts

// Artifact is one large file an application needs, as listed in its manifest.
type Artifact struct {
	ID      string             // "decider-0.8b"
	Version string             // "q4-2026-09"; a new version is a new file
	URL     string             // where to download it; same origin by default ("/artifacts/…")
	Size    int64              // bytes
	SHA256  string             // lowercase hex digest of the whole file
	Needs   device.Requirement // what the device must have; checked by the caller before Ensure
}

// Manifest lists the artifacts an application may download.
type Manifest struct{ Artifacts []Artifact }

// ParseManifest reads the JSON manifest (format below).
func ParseManifest(data []byte) (Manifest, error)

// Find returns the artifact with this id.
func (m Manifest) Find(id string) (Artifact, bool)

// Store keeps artifacts in a file store: OPFS in the browser (one directory per module).
type Store struct{ /* unexported */ }

// New returns a store over fs. Pass opfs.Open("<module>") in the browser, files/mem in tests.
func New(fs files.Store) *Store

// Has reports whether a is stored complete and verified.
func (s *Store) Has(a Artifact) bool

// Ensure downloads a unless it is already stored, resuming a previous partial download, and
// verifies its SHA-256. progress (may be nil) receives bytes done and total after each chunk.
// It returns ErrNoSpace, before downloading anything, when p.Free() is smaller than what is left
// to download. Call it from a goroutine: it blocks on the network.
func (s *Store) Ensure(a Artifact, p device.Profile, progress func(done, total int64)) error

// Read returns the bytes of a stored, verified artifact (for weights.Open), or files.ErrNotExist.
func (s *Store) Read(a Artifact) ([]byte, error)

// Prune deletes every stored version that is not in keep, complete or partial.
func (s *Store) Prune(keep []Artifact) error
```

Errors (comparable values, `files.ErrNotExist` pattern: `var ErrX error = xErr{}`), exact texts:
- `ErrNoSpace` — `artifacts: not enough free space for this download`
- `ErrDigest` — `artifacts: downloaded file does not match its SHA-256` (the partial data is
  removed so the next `Ensure` starts from zero)
- range errors are formatted: `artifacts: %s: server answered %d to a Range request, want 206`
  (URL, status) — a server that ignores `Range` cannot be resumed and must be fixed, not worked around.

### Manifest format (JSON, decoded with `webtyp.com/json`)

```json
{"artifacts":[{"id":"decider-0.8b","version":"q4-2026-09","url":"/artifacts/decider-0.8b.q4.wtypw",
  "size":529000000,"sha256":"…64 hex…",
  "needs":{"min_free":600000000,"min_tier":"simd","min_rate":4,"min_memory_gb":4}}]}
```
`min_tier` is the `device.Tier` string (`plain`, `simd`, `webgpu`; absent = none). `ParseManifest`
errors, exact text: `artifacts: manifest: %s` for invalid JSON; `artifacts: manifest: %s has no %s`
for a missing `id`, `version`, `url`, `size` or `sha256` (id or index, field); `artifacts: manifest:
%s has an invalid sha256` when not 64 lowercase hex characters; `artifacts: manifest: %s is listed
twice` for a repeated `id`+`version`.

### Layout inside the file store

| Path | Content |
|---|---|
| `<id>/<version>` | the artifact bytes (appended chunk by chunk) |
| `<id>/<version>.state` | 8 bytes little-endian offset + the SHA-256 state (`MarshalBinary` of the running `hash.Hash` from `crypto/sha256`) |
| `<id>/<version>.ok` | 64 hex digest, written last: its presence is what `Has` checks |
| `index` | one line `<id>/<version>` per artifact ever started, so `Prune` knows what exists (there is no directory listing in `files`) |

`id` and `version` must be valid path segments (no `/`, not `.`/`..`, not empty): validated by
`ParseManifest` and again by `Ensure` (error `artifacts: invalid id or version %q`).

### `Ensure`, step by step

1. `Has(a)` → return nil.
2. Read `.state`: present → `offset` and hash state restored (`UnmarshalBinary`); absent →
   offset 0, fresh hash, and `RemoveFile` the data file if a stale one exists (ignore
   `files.ErrNotExist`). Add `<id>/<version>` to `index` if missing.
3. `a.Size - offset > p.Free()` → `ErrNoSpace`.
4. Loop while `offset < a.Size`: `end := min(offset+ChunkSize, a.Size) - 1` with
   `const ChunkSize = 8 << 20`; `fetch.Get(a.URL).Header("Range", "bytes=<offset>-<end>")`, wait for
   the callback on a channel; status must be `206` and `len(body) == end-offset+1` (else the formatted
   range error / `artifacts: %s: short chunk`); `AppendFile` data; `hash.Write(body)`; write `.state`;
   `offset += len`; `progress(offset, a.Size)`.
5. Compare `hex(hash.Sum)` with `a.SHA256`: mismatch → remove data and `.state`, return `ErrDigest`.
6. Write `.ok`, remove `.state`.

`crypto/sha256` and `encoding/hex`/`encoding/binary` are allowed here (TinyGo supports them; they are
not on the forbidden list). Do **not** use `encoding/json`.

## Tests (`tests/`, `package tests`)

A test HTTP server is not available in the browser runner, so the download path takes its fetcher
from `webtyp.com/fetch` and the tests run **natively** against `httptest.Server` (`fetch` is
isomorphic: `net/http` natively, browser `fetch()` in wasm) with `files/mem` as the store:

| Test | Proves |
|---|---|
| `TestParseManifest_RoundTrip` | the JSON above → fields, `Needs.MinTier == device.TierSIMD` |
| `TestParseManifest_Errors` | one row per error text |
| `TestEnsure_DownloadsAndVerifies` | 20 MiB random file served with `http.ServeContent` → `Has` true, `Read` equals the file, progress reached `(size, size)` and was called ≥ 3 times |
| `TestEnsure_Resumes` | server that fails after the second chunk; first `Ensure` errors; second `Ensure` against a healthy server requests `Range: bytes=16777216-…` first (record requests) and ends verified |
| `TestEnsure_DigestMismatch` | wrong `SHA256` → `ErrDigest`; `.state` and data gone; `Has` false |
| `TestEnsure_RangeIgnored` | server answering 200 with the whole file → the formatted range error |
| `TestEnsure_NoSpace` | `Profile{Quota: 10, Usage: 0}` → `ErrNoSpace`, zero requests made |
| `TestEnsure_AlreadyStored` | second `Ensure` makes zero requests |
| `TestPrune_KeepsOnlyListed` | two versions stored, one partial → `Prune([]Artifact{v2})` → v1 data, `.ok`, partial and `.state` removed; v2 still `Has` |
| `TestConsumerShaped` | manifest JSON → `Find` → `Needs.Check(profile, rate)` has no blocking shortfall → `Ensure` → `Read` → `weights.Open` of a tiny artifact written with `weights.WriteArtifact` succeeds |

No browser test of its own: `artifacts` only talks to `files.Store`, and `opfs` already passes the
`files` conformance suite (including `Remover`) in the browser. `gotest -tinygo` proves it compiles
for the target.

## Docs

- `README.md`: "I want X → use Y" table and the 15-line flow: parse manifest → find → check
  requirement (`device`) → `device.Persist()` on the user's click (D-PWA-9) → `Ensure` with a
  progress bar → `Read` → `weights.Open` → after switching to a new version, `Prune`.
- `docs/ARCHITECTURE.md`: why OPFS (memory), the layout table, the resume mechanics, why `Prune` is
  explicit and runs only after the new version is in use (D-PWA-8), and the server requirement
  (Range → 206).

## Acceptance

- `gotest` and `gotest -tinygo` green.
- `grep -rn "\"encoding/json\"\|\"net/http\"\|map\[" --include=*.go . | grep -v _test` → empty.
- `grep -rn "webtyp.com/opfs" --include=*.go .` → empty.
- `grep -rn "type Artifacts struct" --include=*.go .` → empty (placeholder deleted).
