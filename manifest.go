package artifacts

import (
	"webtyp.com/device"
	"webtyp.com/fmt"
	"webtyp.com/json"
	"webtyp.com/model"
)

type rawNeeds struct {
	minFree     int64
	minTier     string
	minRate     float64
	minMemoryGB float64
}

func (n *rawNeeds) IsNil() bool { return n == nil }
func (n *rawNeeds) DecodeFields(fr model.FieldReader) {
	if v, ok := fr.Int("min_free"); ok {
		n.minFree = v
	}
	if v, ok := fr.String("min_tier"); ok {
		n.minTier = v
	}
	if v, ok := fr.Float("min_rate"); ok {
		n.minRate = v
	}
	if v, ok := fr.Float("min_memory_gb"); ok {
		n.minMemoryGB = v
	}
}

func (n *rawNeeds) toRequirement() device.Requirement {
	var tier device.Tier
	switch n.minTier {
	case "plain":
		tier = device.TierPlain
	case "simd":
		tier = device.TierSIMD
	case "webgpu":
		tier = device.TierWebGPU
	}
	return device.Requirement{
		MinFree:     n.minFree,
		MinTier:     tier,
		MinRate:     device.Rate(n.minRate),
		MinMemoryGB: n.minMemoryGB,
	}
}

type rawArtifact struct {
	id         string
	version    string
	url        string
	size       int64
	sha256     string
	hasID      bool
	hasVersion bool
	hasURL     bool
	hasSize    bool
	hasSHA256  bool
	needs      device.Requirement
}

func (a *rawArtifact) IsNil() bool { return a == nil }
func (a *rawArtifact) DecodeFields(fr model.FieldReader) {
	if v, ok := fr.String("id"); ok {
		a.id = v
		a.hasID = true
	}
	if v, ok := fr.String("version"); ok {
		a.version = v
		a.hasVersion = true
	}
	if v, ok := fr.String("url"); ok {
		a.url = v
		a.hasURL = true
	}
	if v, ok := fr.Int("size"); ok {
		a.size = v
		a.hasSize = true
	}
	if v, ok := fr.String("sha256"); ok {
		a.sha256 = v
		a.hasSHA256 = true
	}
	if _, ok := fr.Raw("needs"); ok {
		var req rawNeeds
		if fr.Object("needs", &req) {
			a.needs = req.toRequirement()
		}
	}
}

type rawManifest struct {
	artifacts []rawArtifact
}

func (m *rawManifest) IsNil() bool { return m == nil }
func (m *rawManifest) DecodeFields(fr model.FieldReader) {
	if arr, ok := fr.Array("artifacts"); ok {
		m.artifacts = make([]rawArtifact, arr.Len())
		for i := 0; i < arr.Len(); i++ {
			arr.Object(i, &m.artifacts[i])
		}
	}
}

func isValidPathSegment(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] == '/' || s[i] == '\\' {
			return false
		}
	}
	return true
}

func isValidSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		b := s[i]
		if !((b >= '0' && b <= '9') || (b >= 'a' && b <= 'f')) {
			return false
		}
	}
	return true
}

// ParseManifest reads the JSON manifest.
func ParseManifest(data []byte) (Manifest, error) {
	var rm rawManifest
	err := json.Decode(data, &rm)
	if err != nil {
		return Manifest{}, fmt.Errf("artifacts: manifest: %s", err.Error())
	}

	result := Manifest{
		Artifacts: make([]Artifact, 0, len(rm.artifacts)),
	}

	for i, raw := range rm.artifacts {
		var ident string
		if raw.hasID && raw.id != "" {
			ident = raw.id
		} else {
			ident = fmt.Sprintf("artifact %d", i)
		}

		if !raw.hasID {
			return Manifest{}, fmt.Errf("artifacts: manifest: %s has no id", ident)
		}
		if !raw.hasVersion {
			return Manifest{}, fmt.Errf("artifacts: manifest: %s has no version", ident)
		}
		if !raw.hasURL {
			return Manifest{}, fmt.Errf("artifacts: manifest: %s has no url", ident)
		}
		if !raw.hasSize {
			return Manifest{}, fmt.Errf("artifacts: manifest: %s has no size", ident)
		}
		if !raw.hasSHA256 {
			return Manifest{}, fmt.Errf("artifacts: manifest: %s has no sha256", ident)
		}

		if !isValidSHA256(raw.sha256) {
			return Manifest{}, fmt.Errf("artifacts: manifest: %s has an invalid sha256", ident)
		}

		if !isValidPathSegment(raw.id) || !isValidPathSegment(raw.version) {
			return Manifest{}, fmt.Errf("artifacts: manifest: %s has an invalid id or version", ident)
		}

		// Check duplicate id + version
		for _, existing := range result.Artifacts {
			if existing.ID == raw.id && existing.Version == raw.version {
				return Manifest{}, fmt.Errf("artifacts: manifest: %s is listed twice", raw.id)
			}
		}

		result.Artifacts = append(result.Artifacts, Artifact{
			ID:      raw.id,
			Version: raw.version,
			URL:     raw.url,
			Size:    raw.size,
			SHA256:  raw.sha256,
			Needs:   raw.needs,
		})
	}

	return result, nil
}

// Find returns the artifact with this id.
func (m Manifest) Find(id string) (Artifact, bool) {
	for _, a := range m.Artifacts {
		if a.ID == id {
			return a, true
		}
	}
	return Artifact{}, false
}
