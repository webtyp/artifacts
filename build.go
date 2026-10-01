//go:build !wasm

package artifacts

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path"
	"path/filepath"

	"webtyp.com/fmt"
	"webtyp.com/pwa"
)

// LocalFile maps an artifact's URL to the file on the build machine that must be served there.
type LocalFile struct {
	URL  string // pwa.ArtifactsDir + "<id>.<version><ext>"
	Path string // absolute path on disk
}

const (
	errSourceInvalid = "artifacts: source %q: invalid id or version"
	errSourceFile    = "artifacts: source %q: %v"
	errSourceTwice   = "artifacts: source %q is declared twice"
)

// BuildManifest measures every source (size and SHA-256, streamed: the file is never held in
// memory) and returns the manifest plus where each URL's bytes live on disk. root is the module
// root that Source.File is relative to.
func BuildManifest(root string, srcs []Source) (Manifest, []LocalFile, error) {
	var m Manifest
	var files []LocalFile
	for i, s := range srcs {
		if !isValidPathSegment(s.ID) || !isValidPathSegment(s.Version) {
			return Manifest{}, nil, fmt.Errf(errSourceInvalid, s.ID)
		}
		for _, prev := range srcs[:i] {
			if prev.ID == s.ID {
				return Manifest{}, nil, fmt.Errf(errSourceTwice, s.ID)
			}
		}
		abs := s.File
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(root, s.File)
		}
		size, sum, err := measure(abs)
		if err != nil {
			return Manifest{}, nil, fmt.Errf(errSourceFile, s.ID, err)
		}
		url := pwa.ArtifactsDir + s.ID + "." + s.Version + path.Ext(filepath.ToSlash(s.File))
		m.Artifacts = append(m.Artifacts, Artifact{
			ID: s.ID, Version: s.Version, URL: url, Size: size, SHA256: sum, Needs: s.Needs,
		})
		files = append(files, LocalFile{URL: url, Path: abs})
	}
	return m, files, nil
}

func measure(file string) (int64, string, error) {
	f, err := os.Open(file)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return 0, "", err
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}
