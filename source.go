package artifacts

import "webtyp.com/device"

// ManifestPath is where a build serves the manifest of an application's artifacts.
const ManifestPath = "/artifacts.json"

// Source is one artifact as a project declares it, with func ArtifactSources() []artifacts.Source in a
// build-only file of its root package. The compiler (sitec) reads File, measures it and writes the
// manifest; the file itself is never loaded into memory.
type Source struct {
	ID      string             // "decider-0.8b"
	Version string             // "q4-2026-09"; a new version is a new download
	File    string             // path to the file, relative to the module root
	Needs   device.Requirement // what the device must have to download it
}
