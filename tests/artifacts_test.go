package tests

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"webtyp.com/artifacts"
	"webtyp.com/device"
	"webtyp.com/files/mem"
	"webtyp.com/weights"
)

func TestParseManifest_RoundTrip(t *testing.T) {
	js := []byte(`{"artifacts":[{"id":"decider-0.8b","version":"q4-2026-09","url":"/artifacts/decider-0.8b.q4.wtypw",
  "size":529000000,"sha256":"1234567890123456789012345678901234567890123456789012345678901234",
  "needs":{"min_free":600000000,"min_tier":"simd","min_rate":4,"min_memory_gb":4}}]}`)

	m, err := artifacts.ParseManifest(js)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(m.Artifacts))
	}
	a := m.Artifacts[0]
	if a.ID != "decider-0.8b" {
		t.Errorf("got ID %q, want decider-0.8b", a.ID)
	}
	if a.Version != "q4-2026-09" {
		t.Errorf("got Version %q, want q4-2026-09", a.Version)
	}
	if a.Needs.MinTier != device.TierSIMD {
		t.Errorf("got MinTier %v, want TierSIMD", a.Needs.MinTier)
	}
}

func TestParseManifest_Errors(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr string
	}{
		{
			name:    "invalid json",
			json:    `{invalid`,
			wantErr: "artifacts: manifest:",
		},
		{
			name:    "missing id",
			json:    `{"artifacts":[{"version":"v1","url":"/u","size":10,"sha256":"1234567890123456789012345678901234567890123456789012345678901234"}]}`,
			wantErr: "artifacts: manifest: artifact 0 has no id",
		},
		{
			name:    "missing version",
			json:    `{"artifacts":[{"id":"decider-0.8b","url":"/u","size":10,"sha256":"1234567890123456789012345678901234567890123456789012345678901234"}]}`,
			wantErr: "artifacts: manifest: decider-0.8b has no version",
		},
		{
			name:    "missing url",
			json:    `{"artifacts":[{"id":"decider-0.8b","version":"v1","size":10,"sha256":"1234567890123456789012345678901234567890123456789012345678901234"}]}`,
			wantErr: "artifacts: manifest: decider-0.8b has no url",
		},
		{
			name:    "missing size",
			json:    `{"artifacts":[{"id":"decider-0.8b","version":"v1","url":"/u","sha256":"1234567890123456789012345678901234567890123456789012345678901234"}]}`,
			wantErr: "artifacts: manifest: decider-0.8b has no size",
		},
		{
			name:    "missing sha256",
			json:    `{"artifacts":[{"id":"decider-0.8b","version":"v1","url":"/u","size":10}]}`,
			wantErr: "artifacts: manifest: decider-0.8b has no sha256",
		},
		{
			name:    "invalid sha256",
			json:    `{"artifacts":[{"id":"decider-0.8b","version":"v1","url":"/u","size":10,"sha256":"bad"}]}`,
			wantErr: "artifacts: manifest: decider-0.8b has an invalid sha256",
		},
		{
			name: "duplicate id and version",
			json: `{"artifacts":[
				{"id":"decider-0.8b","version":"v1","url":"/u","size":10,"sha256":"1234567890123456789012345678901234567890123456789012345678901234"},
				{"id":"decider-0.8b","version":"v1","url":"/u2","size":10,"sha256":"1234567890123456789012345678901234567890123456789012345678901234"}
			]}`,
			wantErr: "artifacts: manifest: decider-0.8b is listed twice",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := artifacts.ParseManifest([]byte(tt.json))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got error %q, want string containing %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestEnsure_DownloadsAndVerifies(t *testing.T) {
	data := make([]byte, 20*1024*1024)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("failed to generate rand data: %v", err)
	}
	hash := sha256.Sum256(data)
	digest := hex.EncodeToString(hash[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "artifact.bin", time.Time{}, bytes.NewReader(data))
	}))
	defer server.Close()

	mfs := mem.New()
	store := artifacts.New(mfs)

	a := artifacts.Artifact{
		ID:      "test-model",
		Version: "v1",
		URL:     server.URL + "/artifact.bin",
		Size:    int64(len(data)),
		SHA256:  digest,
	}

	var progressCount int32
	var lastDone int64
	err := store.Ensure(a, device.Profile{Secure: true, Quota: 100 * 1024 * 1024}, func(done, total int64) {
		atomic.AddInt32(&progressCount, 1)
		atomic.StoreInt64(&lastDone, done)
		if total != int64(len(data)) {
			t.Errorf("progress total = %d, want %d", total, len(data))
		}
	})

	if err != nil {
		t.Fatalf("Ensure failed: %v", err)
	}

	if !store.Has(a) {
		t.Fatalf("expected store.Has to return true")
	}

	readBytes, err := store.Read(a)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if !bytes.Equal(readBytes, data) {
		t.Fatalf("read bytes mismatch")
	}

	if atomic.LoadInt32(&progressCount) < 3 {
		t.Errorf("expected at least 3 progress calls, got %d", progressCount)
	}
	if atomic.LoadInt64(&lastDone) != int64(len(data)) {
		t.Errorf("last progress done = %d, want %d", lastDone, len(data))
	}
}

func TestEnsure_Resumes(t *testing.T) {
	data := make([]byte, 20*1024*1024)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("failed to generate rand data: %v", err)
	}
	hash := sha256.Sum256(data)
	digest := hex.EncodeToString(hash[:])

	var chunkCount int32
	failingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&chunkCount, 1)
		if count > 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		http.ServeContent(w, r, "artifact.bin", time.Time{}, bytes.NewReader(data))
	}))
	defer failingServer.Close()

	mfs := mem.New()
	store := artifacts.New(mfs)

	a := artifacts.Artifact{
		ID:      "resume-model",
		Version: "v1",
		URL:     failingServer.URL + "/artifact.bin",
		Size:    int64(len(data)),
		SHA256:  digest,
	}

	err := store.Ensure(a, device.Profile{Secure: true, Quota: 100 * 1024 * 1024}, nil)
	if err == nil {
		t.Fatalf("expected error from failing server, got nil")
	}

	var requestedRanges []string
	healthyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeH := r.Header.Get("Range")
		if rangeH != "" {
			requestedRanges = append(requestedRanges, rangeH)
		}
		http.ServeContent(w, r, "artifact.bin", time.Time{}, bytes.NewReader(data))
	}))
	defer healthyServer.Close()

	a.URL = healthyServer.URL + "/artifact.bin"
	err = store.Ensure(a, device.Profile{Secure: true, Quota: 100 * 1024 * 1024}, nil)
	if err != nil {
		t.Fatalf("Ensure resume failed: %v", err)
	}

	if !store.Has(a) {
		t.Fatalf("expected store.Has to return true after resume")
	}

	if len(requestedRanges) == 0 {
		t.Fatalf("expected Range requests on healthy server")
	}
	expectedOffset := 2 * (8 * 1024 * 1024)
	if !strings.HasPrefix(requestedRanges[0], "bytes="+strconv.Itoa(expectedOffset)+"-") {
		t.Fatalf("first requested range = %q, want start at %d", requestedRanges[0], expectedOffset)
	}
}

func TestEnsure_DigestMismatch(t *testing.T) {
	data := []byte("hello world mismatch testing")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "a.bin", time.Time{}, bytes.NewReader(data))
	}))
	defer server.Close()

	mfs := mem.New()
	store := artifacts.New(mfs)

	a := artifacts.Artifact{
		ID:      "bad-digest",
		Version: "v1",
		URL:     server.URL + "/a.bin",
		Size:    int64(len(data)),
		SHA256:  "0000000000000000000000000000000000000000000000000000000000000000",
	}

	err := store.Ensure(a, device.Profile{Secure: true, Quota: 100 * 1024 * 1024}, nil)
	if err != artifacts.ErrDigest {
		t.Fatalf("got err %v, want ErrDigest", err)
	}

	if store.Has(a) {
		t.Fatalf("expected Has to be false")
	}

	if _, err := mfs.ReadFile("bad-digest/v1.state"); err == nil {
		t.Fatalf("expected state file to be removed")
	}
	if _, err := mfs.ReadFile("bad-digest/v1"); err == nil {
		t.Fatalf("expected data file to be removed")
	}
}

func TestEnsure_RangeIgnored(t *testing.T) {
	data := make([]byte, 1000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	}))
	defer server.Close()

	mfs := mem.New()
	store := artifacts.New(mfs)

	a := artifacts.Artifact{
		ID:      "range-ignored",
		Version: "v1",
		URL:     server.URL + "/a.bin",
		Size:    int64(len(data)),
		SHA256:  "1234567890123456789012345678901234567890123456789012345678901234",
	}

	err := store.Ensure(a, device.Profile{Secure: true, Quota: 100 * 1024 * 1024}, nil)
	if err == nil || !strings.Contains(err.Error(), "server answered 200 to a Range request, want 206") {
		t.Fatalf("got err %v, want Range request error", err)
	}
}

func TestEnsure_NoSpace(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mfs := mem.New()
	store := artifacts.New(mfs)

	a := artifacts.Artifact{
		ID:      "no-space",
		Version: "v1",
		URL:     server.URL + "/a.bin",
		Size:    100,
		SHA256:  "1234567890123456789012345678901234567890123456789012345678901234",
	}

	err := store.Ensure(a, device.Profile{Secure: true, Quota: 10, Usage: 0}, nil)
	if err != artifacts.ErrNoSpace {
		t.Fatalf("got err %v, want ErrNoSpace", err)
	}

	if atomic.LoadInt32(&requestCount) != 0 {
		t.Fatalf("expected 0 requests made, got %d", requestCount)
	}
}

func TestEnsure_AlreadyStored(t *testing.T) {
	data := []byte("stored data")
	hash := sha256.Sum256(data)
	digest := hex.EncodeToString(hash[:])

	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		http.ServeContent(w, r, "a.bin", time.Time{}, bytes.NewReader(data))
	}))
	defer server.Close()

	mfs := mem.New()
	store := artifacts.New(mfs)

	a := artifacts.Artifact{
		ID:      "already-stored",
		Version: "v1",
		URL:     server.URL + "/a.bin",
		Size:    int64(len(data)),
		SHA256:  digest,
	}

	err := store.Ensure(a, device.Profile{Secure: true, Quota: 100 * 1024 * 1024}, nil)
	if err != nil {
		t.Fatalf("first Ensure failed: %v", err)
	}

	atomic.StoreInt32(&requestCount, 0)
	err = store.Ensure(a, device.Profile{Secure: true, Quota: 100 * 1024 * 1024}, nil)
	if err != nil {
		t.Fatalf("second Ensure failed: %v", err)
	}

	if atomic.LoadInt32(&requestCount) != 0 {
		t.Fatalf("expected 0 requests on second Ensure, got %d", requestCount)
	}
}

func TestPrune_KeepsOnlyListed(t *testing.T) {
	mfs := mem.New()
	store := artifacts.New(mfs)

	v1Data := []byte("v1 data")
	v1Hash := sha256.Sum256(v1Data)
	v1Digest := hex.EncodeToString(v1Hash[:])

	v2Data := []byte("v2 data")
	v2Hash := sha256.Sum256(v2Data)
	v2Digest := hex.EncodeToString(v2Hash[:])

	s1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "v1", time.Time{}, bytes.NewReader(v1Data))
	}))
	defer s1.Close()

	s2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "v2", time.Time{}, bytes.NewReader(v2Data))
	}))
	defer s2.Close()

	aV1 := artifacts.Artifact{ID: "m", Version: "v1", URL: s1.URL + "/v1", Size: int64(len(v1Data)), SHA256: v1Digest}
	aV2 := artifacts.Artifact{ID: "m", Version: "v2", URL: s2.URL + "/v2", Size: int64(len(v2Data)), SHA256: v2Digest}

	if err := store.Ensure(aV1, device.Profile{Secure: true, Quota: 100 * 1024 * 1024}, nil); err != nil {
		t.Fatalf("Ensure v1 failed: %v", err)
	}
	if err := store.Ensure(aV2, device.Profile{Secure: true, Quota: 100 * 1024 * 1024}, nil); err != nil {
		t.Fatalf("Ensure v2 failed: %v", err)
	}

	// Add partial v3 state
	_ = mfs.WriteFile("m/v3", []byte("partial"))
	_ = mfs.WriteFile("m/v3.state", []byte("state"))
	_ = mfs.AppendFile("index", []byte("m/v3\n"))

	if err := store.Prune([]artifacts.Artifact{aV2}); err != nil {
		t.Fatalf("Prune failed: %v", err)
	}

	if store.Has(aV1) {
		t.Fatalf("v1 should have been pruned")
	}
	if !store.Has(aV2) {
		t.Fatalf("v2 should have been kept")
	}

	if _, err := mfs.ReadFile("m/v3"); err == nil {
		t.Fatalf("v3 partial data should have been pruned")
	}
	if _, err := mfs.ReadFile("m/v3.state"); err == nil {
		t.Fatalf("v3 partial state should have been pruned")
	}
}

func TestConsumerShaped(t *testing.T) {
	tok := weights.TokenizerConfig{Lowercase: true}
	inputs := []weights.TensorInput{
		{
			Name:  "test_tensor",
			DType: weights.Float32,
			Shape: []int{1, 4},
			Data:  []byte{0, 0, 128, 63, 0, 0, 0, 64, 0, 0, 64, 64, 0, 0, 128, 64}, // 1.0, 2.0, 3.0, 4.0
		},
	}
	weightBytes, err := weights.WriteArtifact("tiny-model", 1, tok, inputs)
	if err != nil {
		t.Fatalf("WriteArtifact failed: %v", err)
	}
	hash := sha256.Sum256(weightBytes)
	digest := hex.EncodeToString(hash[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "model.wtypw", time.Time{}, bytes.NewReader(weightBytes))
	}))
	defer server.Close()

	js := []byte(`{"artifacts":[{
		"id":"tiny-model",
		"version":"v1",
		"url":"` + server.URL + `/model.wtypw",
		"size":` + strconv.FormatInt(int64(len(weightBytes)), 10) + `,
		"sha256":"` + digest + `",
		"needs":{"min_free":1000,"min_tier":"simd"}
	}]}`)

	m, err := artifacts.ParseManifest(js)
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}

	art, ok := m.Find("tiny-model")
	if !ok {
		t.Fatalf("Find failed to locate tiny-model")
	}

	profile := device.Profile{
		Secure: true,
		Quota:  100 * 1024 * 1024,
		Usage:  0,
		SIMD:   true,
	}
	shortfalls := art.Needs.Check(profile, 0)
	for _, sf := range shortfalls {
		if sf.Blocking() {
			t.Fatalf("blocking shortfall: %v", sf)
		}
	}

	mfs := mem.New()
	store := artifacts.New(mfs)

	if err := store.Ensure(art, profile, nil); err != nil {
		t.Fatalf("Ensure failed: %v", err)
	}

	rawBytes, err := store.Read(art)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	modelArt, err := weights.Open(rawBytes)
	if err != nil {
		t.Fatalf("weights.Open failed: %v", err)
	}

	if modelArt.ID != "tiny-model" {
		t.Fatalf("modelArt.ID = %q, want tiny-model", modelArt.ID)
	}
	tensor, ok := modelArt.Tensor("test_tensor")
	if !ok {
		t.Fatalf("tensor test_tensor not found")
	}
	if tensor.DType != weights.Float32 {
		t.Fatalf("tensor DType = %v, want Float32", tensor.DType)
	}
}
