package artifacts

import (
	"webtyp.com/device"
	"webtyp.com/files"
)

// Artifact is one large file an application needs, as listed in its manifest.
type Artifact struct {
	ID      string             `json:"id"`
	Version string             `json:"version"`
	URL     string             `json:"url"`
	Size    int64              `json:"size"`
	SHA256  string             `json:"sha256"`
	Needs   device.Requirement `json:"needs"`
}

// Manifest lists the artifacts an application may download.
type Manifest struct {
	Artifacts []Artifact `json:"artifacts"`
}

// Store keeps artifacts in a file store: OPFS in the browser (one directory per module).
type Store struct {
	fs files.Store
}

// New returns a store over fs. Pass opfs.Open("<module>") in the browser, files/mem in tests.
func New(fs files.Store) *Store {
	return &Store{fs: fs}
}

type errNoSpace struct{}

func (errNoSpace) Error() string {
	return "artifacts: not enough free space for this download"
}

// ErrNoSpace is returned by Ensure before downloading anything when the profile's Free space is smaller than what is left to download.
var ErrNoSpace error = errNoSpace{}

type errDigest struct{}

func (errDigest) Error() string {
	return "artifacts: downloaded file does not match its SHA-256"
}

// ErrDigest is returned when the downloaded file does not match its SHA-256 checksum.
var ErrDigest error = errDigest{}
