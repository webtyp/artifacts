# Architecture — `webtyp/artifacts`

This document describes the design and storage mechanics for large artifacts (400–850 MB model weights) managed by `webtyp/artifacts`.

## Why OPFS over IndexedDB or Cache Storage

In-browser model execution requires holding large weight files in storage.
- **IndexedDB** and **Cache Storage** hand back the entire file into memory when queried, doubling RAM consumption and causing severe memory pressure or OOM crashes in browser Workers.
- **OPFS (Origin Private File System)** allows fast file access, direct streaming, and efficient disk storage inside dedicated worker threads via filesystem access handles.

## Layout inside the File Store

Artifacts are kept under the `files.Store` passed to `artifacts.New(fs)`. In the browser, this is backed by `opfs.Open("<module>")`.

| Path | Content |
|---|---|
| `<id>/<version>` | Artifact raw payload bytes, appended chunk by chunk (8 MiB chunks) |
| `<id>/<version>.state` | 8 bytes little-endian offset + SHA-256 state (`MarshalBinary` of running `sha256.Hash`) |
| `<id>/<version>.ok` | 64 lowercase hex digest string, written upon complete download and SHA-256 verification |
| `index` | List of `<id>/<version>` entries ever started or stored, enabling `Prune` to identify stale files |

## Resume Mechanics

When `Ensure(a, profile, progress)` is invoked:
1. `Has(a)` checks if `<id>/<version>.ok` exists and contains the expected SHA-256 digest.
2. If absent or unverified, `Ensure` reads `<id>/<version>.state`.
   - If present and valid, `offset` and the running SHA-256 state are restored.
   - If absent or corrupt, `offset` resets to `0`, state is initialized, and partial data file is removed.
3. Downloads proceed in 8 MiB `Range: bytes=<offset>-<end>` chunks.
4. Each chunk is appended to `<id>/<version>`, written to the running SHA-256 hash, and saved back to `<id>/<version>.state`.

## HTTP Server Requirement

- The server delivering artifacts must support RFC 9110 HTTP Range requests (`Range: bytes=from-to`).
- The server MUST respond with HTTP status `206 Partial Content`.
- If a server responds with HTTP status `200` (ignoring `Range`), `Ensure` aborts with an error (`server answered 200 to a Range request, want 206`). Resuming cannot work when `Range` is ignored.

## Explicit Pruning

Pruning is explicit (`store.Prune(keep)`). The library never automatically deletes old versions during `Ensure` to ensure active sessions using an older version remain uninterrupted. `Prune` should be called by the application only after a newly downloaded version is verified and in active use.
