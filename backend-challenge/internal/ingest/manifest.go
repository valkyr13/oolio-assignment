package ingest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// manifestFormat is bumped if the layout of the state directory changes.
const manifestFormat = 1

// Manifest is the record of what has been ingested. It is the single source
// of truth: Build only counts files listed here, so shards left behind by a
// crash are ignored.
type Manifest struct {
	Format  int `json:"format"`
	Buckets int `json:"buckets"`
	// Shards only hold codes that passed the length rule in force when they
	// were written, so the length rule is part of the state.
	MinLen int          `json:"minLen"`
	MaxLen int          `json:"maxLen"`
	Files  []FileRecord `json:"files"`
}

// FileRecord describes one ingested source file.
type FileRecord struct {
	ID      string    `json:"id"`      // SHA-256 of the file's bytes, in hex
	Name    string    `json:"name"`    // file name when it was added, for people
	Size    int64     `json:"size"`    // bytes
	Codes   int64     `json:"codes"`   // codes kept (right length)
	Skipped int64     `json:"skipped"` // lines dropped by the length rule
	AddedAt time.Time `json:"addedAt"`
}

func (m Manifest) has(id string) bool {
	for _, f := range m.Files {
		if f.ID == id {
			return true
		}
	}
	return false
}

// compatible reports whether existing state can be used with cfg.
func (m Manifest) compatible(cfg Config) error {
	if m.Format != manifestFormat {
		return fmt.Errorf("state format %d is not supported (want %d)", m.Format, manifestFormat)
	}
	if m.Buckets != cfg.Buckets {
		return fmt.Errorf("state was built with %d buckets, not %d: shards with different bucket counts cannot be mixed", m.Buckets, cfg.Buckets)
	}
	if m.MinLen != cfg.Rule.MinLen || m.MaxLen != cfg.Rule.MaxLen {
		return fmt.Errorf("state was built for codes of %d-%d characters, not %d-%d: its shards hold only the original lengths, so ingest into a new state directory",
			m.MinLen, m.MaxLen, cfg.Rule.MinLen, cfg.Rule.MaxLen)
	}
	return nil
}

func loadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("reading %s: %w", path, err)
	}
	return m, nil
}

func saveManifest(path string, m Manifest) error {
	return writeFileAtomic(path, 0o644, func(w *bufio.Writer) error {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(m)
	})
}

// writeFileAtomic writes a file so that readers see either the old version or
// the complete new one, never a partial file: it writes a temporary file in
// the same directory, flushes it to disk, then renames it over the target.
func writeFileAtomic(path string, perm os.FileMode, write func(w *bufio.Writer) error) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	w := bufio.NewWriter(tmp)
	if err = write(w); err != nil {
		return err
	}
	if err = w.Flush(); err != nil {
		return err
	}
	// CreateTemp makes files readable by the owner only; the published list
	// must also be readable by the server, which may run as another user.
	if err = tmp.Chmod(perm); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
