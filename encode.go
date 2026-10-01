package artifacts

import (
	"webtyp.com/device"
	"webtyp.com/json"
	"webtyp.com/model"
)

// Encode writes the manifest in the JSON format ParseManifest reads.
func (m Manifest) Encode() ([]byte, error) {
	var out []byte
	err := json.Encode(manifestOut{m}, &out)
	return out, err
}

type manifestOut struct{ m Manifest }

func (o manifestOut) IsNil() bool { return false }
func (o manifestOut) EncodeFields(w model.FieldWriter) {
	arr := w.Array("artifacts", len(o.m.Artifacts))
	for _, a := range o.m.Artifacts {
		arr.Object(artifactOut{a})
	}
	arr.Close()
}

type artifactOut struct{ a Artifact }

func (o artifactOut) IsNil() bool { return false }
func (o artifactOut) EncodeFields(w model.FieldWriter) {
	w.String("id", o.a.ID)
	w.String("version", o.a.Version)
	w.String("url", o.a.URL)
	w.Int("size", o.a.Size)
	w.String("sha256", o.a.SHA256)
	w.Object("needs", needsOut{o.a.Needs})
}

type needsOut struct{ r device.Requirement }

func (o needsOut) IsNil() bool { return false }
func (o needsOut) EncodeFields(w model.FieldWriter) {
	if o.r.MinFree > 0 {
		w.Int("min_free", o.r.MinFree)
	}
	if o.r.MinTier > device.TierNone {
		w.String("min_tier", o.r.MinTier.String())
	}
	if o.r.MinRate > 0 {
		w.Float("min_rate", float64(o.r.MinRate))
	}
	if o.r.MinMemoryGB > 0 {
		w.Float("min_memory_gb", o.r.MinMemoryGB)
	}
}
