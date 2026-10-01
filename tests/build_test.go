//go:build !wasm

package tests

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webtyp.com/artifacts"
	"webtyp.com/device"
	"webtyp.com/pwa"
)

// What a build writes is exactly what the browser reads: BuildManifest → Encode → ParseManifest.
func TestBuildManifest_RoundTrip(t *testing.T) {
	root := t.TempDir()
	content := []byte("weights of decider")
	if err := os.MkdirAll(filepath.Join(root, "models"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "models", "decider.wtypw"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	srcs := []artifacts.Source{{
		ID: "decider-0.8b", Version: "q4-2026-09", File: "models/decider.wtypw",
		Needs: device.Requirement{MinFree: 600, MinTier: device.TierSIMD, MinRate: 4, MinMemoryGB: 4},
	}}
	m, files, err := artifacts.BuildManifest(root, srcs)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	want := artifacts.Artifact{
		ID: "decider-0.8b", Version: "q4-2026-09",
		URL:  pwa.ArtifactsDir + "decider-0.8b.q4-2026-09.wtypw",
		Size: int64(len(content)), SHA256: hex.EncodeToString(sum[:]), Needs: srcs[0].Needs,
	}
	if len(m.Artifacts) != 1 || m.Artifacts[0] != want {
		t.Fatalf("manifest = %+v, want %+v", m.Artifacts, want)
	}
	if len(files) != 1 || files[0].URL != want.URL || files[0].Path != filepath.Join(root, "models", "decider.wtypw") {
		t.Fatalf("files = %+v", files)
	}
	data, err := m.Encode()
	if err != nil {
		t.Fatal(err)
	}
	back, err := artifacts.ParseManifest(data)
	if err != nil {
		t.Fatalf("ParseManifest(Encode()) = %v\n%s", err, data)
	}
	if len(back.Artifacts) != 1 || back.Artifacts[0] != want {
		t.Fatalf("round trip = %+v, want %+v\n%s", back.Artifacts, want, data)
	}
}

func TestBuildManifest_Errors(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.bin"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		srcs []artifacts.Source
		want string
	}{
		{"missing file", []artifacts.Source{{ID: "a", Version: "1", File: "nope.bin"}}, `source "a"`},
		{"invalid id", []artifacts.Source{{ID: "a/b", Version: "1", File: "a.bin"}}, "invalid id or version"},
		{"twice", []artifacts.Source{{ID: "a", Version: "1", File: "a.bin"}, {ID: "a", Version: "2", File: "a.bin"}}, "declared twice"},
	}
	for _, c := range cases {
		if _, _, err := artifacts.BuildManifest(root, c.srcs); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want it to contain %q", c.name, err, c.want)
		}
	}
}
