# artifacts
<img src="docs/img/badges.svg">

`artifacts` downloads and retains large browser files (model weights, decision caches, indexes) up to hundreds of megabytes in OPFS. Downloads happen once, resume after closed tabs or network drops, and are verified against SHA-256 before use.

## I want X → use Y

| I want to ... | Use ... |
|---|---|
| Parse a JSON manifest listing model files | `artifacts.ParseManifest(data)` |
| Look up an artifact by ID in a manifest | `manifest.Find("decider-0.8b")` |
| Check if an artifact is stored and verified | `store.Has(a)` |
| Download an artifact with progress and resume | `store.Ensure(a, profile, progress)` |
| Read stored bytes for `weights.Open` | `store.Read(a)` |
| Delete old versions and unneeded partials | `store.Prune(keep)` |

## Flow

```go
// 1. Parse manifest and find artifact
manifest, err := artifacts.ParseManifest(manifestJSON)
a, ok := manifest.Find("decider-0.8b")

// 2. Check device requirements
if shortfalls := a.Needs.Check(profile, measuredRate); len(shortfalls) > 0 {
    // handle shortfalls...
}

// 3. User gesture to persist storage
// device.Persist()

// 4. Download and verify with progress
store := artifacts.New(opfsStore)
err = store.Ensure(a, profile, func(done, total int64) {
    updateProgressBar(float64(done) / float64(total))
})

// 5. Read verified weights
data, err := store.Read(a)
model, err := weights.Open(data)

// 6. Delete old artifact versions after switching
_ = store.Prune([]artifacts.Artifact{a})
```
