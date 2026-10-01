package artifacts

import (
	"crypto/sha256"
	"encoding"
	"encoding/binary"
	"encoding/hex"

	"webtyp.com/device"
	"webtyp.com/fetch"
	"webtyp.com/files"
	"webtyp.com/fmt"
)

const chunkSize = 8 << 20 // 8 MiB

// Has reports whether a is stored complete and verified.
func (s *Store) Has(a Artifact) bool {
	okPath := a.ID + "/" + a.Version + ".ok"
	data, err := s.fs.ReadFile(okPath)
	if err != nil {
		return false
	}
	return fmt.Convert(string(data)).TrimSpace().String() == a.SHA256
}

// Read returns the bytes of a stored, verified artifact (for weights.Open), or files.ErrNotExist.
func (s *Store) Read(a Artifact) ([]byte, error) {
	if !s.Has(a) {
		return nil, files.ErrNotExist
	}
	return s.fs.ReadFile(a.ID + "/" + a.Version)
}

// Ensure downloads a unless it is already stored, resuming a previous partial download, and
// verifies its SHA-256. progress (may be nil) receives bytes done and total after each chunk.
// It returns ErrNoSpace, before downloading anything, when p.Free() is smaller than what is left
// to download. Call it from a goroutine: it blocks on the network.
func (s *Store) Ensure(a Artifact, p device.Profile, progress func(done, total int64)) error {
	if !isValidPathSegment(a.ID) {
		return fmt.Errf("artifacts: invalid id or version %q", a.ID)
	}
	if !isValidPathSegment(a.Version) {
		return fmt.Errf("artifacts: invalid id or version %q", a.Version)
	}

	if s.Has(a) {
		return nil
	}

	dataPath := a.ID + "/" + a.Version
	statePath := a.ID + "/" + a.Version + ".state"
	okPath := a.ID + "/" + a.Version + ".ok"
	key := a.ID + "/" + a.Version

	h := sha256.New()
	var offset int64 = 0

	stateData, err := s.fs.ReadFile(statePath)
	if err == nil && len(stateData) >= 8 {
		offset = int64(binary.LittleEndian.Uint64(stateData[:8]))
		if unmarshaler, ok := h.(encoding.BinaryUnmarshaler); ok {
			if unmarshaler.UnmarshalBinary(stateData[8:]) != nil {
				offset = 0
				h = sha256.New()
				_ = s.fs.RemoveFile(dataPath)
			}
		} else {
			offset = 0
			h = sha256.New()
			_ = s.fs.RemoveFile(dataPath)
		}
	} else {
		offset = 0
		h = sha256.New()
		_ = s.fs.RemoveFile(dataPath)
	}

	// Update index file if key is missing
	idxData, idxErr := s.fs.ReadFile("index")
	hasKey := false
	if idxErr == nil {
		lines := fmt.Convert(string(idxData)).Split("\n")
		for _, line := range lines {
			if fmt.Convert(line).TrimSpace().String() == key {
				hasKey = true
				break
			}
		}
	}
	if !hasKey {
		_ = s.fs.AppendFile("index", []byte(key+"\n"))
	}

	needed := a.Size - offset
	if needed > p.Free() {
		return ErrNoSpace
	}

	for offset < a.Size {
		end := offset + chunkSize
		if end > a.Size {
			end = a.Size
		}
		rangeHeader := fmt.Sprintf("bytes=%d-%d", offset, end-1)

		type fetchResult struct {
			resp *fetch.Response
			err  error
		}
		ch := make(chan fetchResult, 1)

		fetch.Get(a.URL).Header("Range", rangeHeader).Send(func(resp *fetch.Response, err error) {
			ch <- fetchResult{resp: resp, err: err}
		})

		res := <-ch
		if res.err != nil {
			return res.err
		}

		if res.resp.Status != 206 {
			return fmt.Errf("artifacts: %s: server answered %d to a Range request, want 206", a.URL, res.resp.Status)
		}

		body := res.resp.Body()
		expectedLen := end - offset
		if int64(len(body)) != expectedLen {
			return fmt.Errf("artifacts: %s: short chunk", a.URL)
		}

		if err := s.fs.AppendFile(dataPath, body); err != nil {
			return err
		}

		h.Write(body)
		offset += int64(len(body))

		marshaler, ok := h.(encoding.BinaryMarshaler)
		if !ok {
			return fmt.Errf("artifacts: sha256 state marshaling not supported")
		}
		hashState, err := marshaler.MarshalBinary()
		if err != nil {
			return err
		}

		stateBuf := make([]byte, 8+len(hashState))
		binary.LittleEndian.PutUint64(stateBuf[:8], uint64(offset))
		copy(stateBuf[8:], hashState)

		if err := s.fs.WriteFile(statePath, stateBuf); err != nil {
			return err
		}

		if progress != nil {
			progress(offset, a.Size)
		}
	}

	digest := hex.EncodeToString(h.Sum(nil))
	if digest != a.SHA256 {
		_ = s.fs.RemoveFile(dataPath)
		_ = s.fs.RemoveFile(statePath)
		return ErrDigest
	}

	if err := s.fs.WriteFile(okPath, []byte(a.SHA256)); err != nil {
		return err
	}
	_ = s.fs.RemoveFile(statePath)

	return nil
}

// Prune deletes every stored version that is not in keep, complete or partial.
func (s *Store) Prune(keep []Artifact) error {
	idxData, err := s.fs.ReadFile("index")
	if err != nil {
		return nil
	}

	lines := fmt.Convert(string(idxData)).Split("\n")
	var newIndex string

	for _, line := range lines {
		key := fmt.Convert(line).TrimSpace().String()
		if key == "" {
			continue
		}

		shouldKeep := false
		for _, k := range keep {
			if k.ID+"/"+k.Version == key {
				shouldKeep = true
				break
			}
		}

		if shouldKeep {
			newIndex += key + "\n"
		} else {
			_ = s.fs.RemoveFile(key)
			_ = s.fs.RemoveFile(key + ".state")
			_ = s.fs.RemoveFile(key + ".ok")
		}
	}

	return s.fs.WriteFile("index", []byte(newIndex))
}
