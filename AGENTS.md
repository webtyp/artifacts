# AGENTS.md — webtyp/artifacts

Working notes for AI agents operating in this library. End-user docs: [README.md](README.md).

## Mission

`artifacts` keeps the **large files** of a webtyp application in the browser — model weights,
decision caches, indexes (hundreds of MB). It reads a manifest (id, version, URL, size, SHA-256,
device requirement), downloads each artifact **once** in 8 MiB `Range` chunks that resume after a
closed tab, verifies the SHA-256 before the artifact is usable, stores it through `files.Store`
(OPFS in the browser), and deletes old versions only when told (`Prune`).

It does not decide whether the device is good enough (that is `webtyp/device`), does not open
weights (that is `webtyp/weights`), and never imports `webtyp/opfs`: the application passes
`opfs.Open("<module>")` as a `files.Store`.

## The build that decides

This is a **browser library**. It is compiled by TinyGo to WebAssembly in production. A change is
done only when all of these pass:

```bash
go install webtyp.com/devflow/cmd/gotest@latest   # once; needs gotest >= v0.4.108
gotest            # vet + tests + the wasm tests in tests/ run in a headless browser
gotest -tinygo    # also compiles with TinyGo — mandatory
```

`GOOS=js GOARCH=wasm go build` succeeding proves nothing: it uses the **full** standard library and
does not imply TinyGo. `net/http` compiles there and is still forbidden.

## Forbidden imports and their replacement

| Do not import | Use instead |
|---|---|
| `fmt`, `errors`, `strconv`, `strings` | `webtyp.com/fmt` |
| `encoding/json` (adds ~1 MB of wasm under TinyGo) | `webtyp.com/json` |
| `net/http` | `webtyp.com/fetch` |
| `context` | `webtyp.com/context` |
| `time` | `webtyp.com/time`, or `performance.now()` through `syscall/js` |
| `reflect` | nothing — plain structs |
| `map[K]V` (heavy in TinyGo) | a slice of structs scanned linearly, or `fmt.KeyValue` |

Waiting on a JavaScript promise: `webtyp.com/await` (`await.Promise(p)`), never a hand-written
channel + callback pair. Do not invent a port or helper that another `webtyp.com/*` package
already exposes.

## Tests

All tests in `tests/` (`package tests`, public API only). They run natively against
`net/http/httptest` with `files/mem` as the store: `net/http` is fine **in tests**, never in library
code (`webtyp.com/fetch` is the client, and it is isomorphic).

## Build-time files

`build.go` (`//go:build !wasm`) runs inside the compiler (`sitec`) on the developer's machine:
`BuildManifest` streams each declared file through SHA-256 with `os`/`io`. That stdlib use is
legitimate there and never reaches the browser binary.
