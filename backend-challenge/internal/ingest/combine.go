package ingest

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// BuildStats summarises a Build.
type BuildStats struct {
	Files   int // ingested files that were counted
	Buckets int
	Valid   int // codes published
	Took    time.Duration
}

// Build counts, bucket by bucket, how many ingested files each code appears
// in, and publishes the codes that reach Rule.MinFiles to outPath, one per
// line. Only one bucket is in memory at a time, and each bucket's codes are
// written as soon as they are known, so memory does not grow with the number
// of valid codes either.
//
// Codes are sorted within each bucket, so the output is the same on every
// run; sort the file if you need it in global order.
//
// The file is published atomically: readers see the old list or the complete
// new one. If Build fails, the old list stays in place.
func (in *Ingester) Build(outPath string) (BuildStats, error) {
	start := time.Now()
	in.mu.Lock()
	m := in.manifest
	in.mu.Unlock()
	if len(m.Files) == 0 {
		return BuildStats{}, errors.New("no files have been ingested yet")
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return BuildStats{}, err
	}
	valid := 0
	err := writeFileAtomic(outPath, 0o644, func(w *bufio.Writer) error {
		for b := 0; b < m.Buckets; b++ {
			codes, err := in.combineBucket(m, b)
			if err != nil {
				return fmt.Errorf("bucket %d: %w", b, err)
			}
			for _, code := range codes {
				if _, err := w.WriteString(code); err != nil {
					return err
				}
				if err := w.WriteByte('\n'); err != nil {
					return err
				}
			}
			valid += len(codes)
		}
		return nil
	})
	if err != nil {
		return BuildStats{}, err
	}
	return BuildStats{Files: len(m.Files), Buckets: m.Buckets, Valid: valid, Took: time.Since(start)}, nil
}

// combineBucket returns the codes in one bucket that appear in at least
// Rule.MinFiles different files, sorted.
func (in *Ingester) combineBucket(m Manifest, bucket int) ([]string, error) {
	// For each code: how many files it has been seen in, and which file
	// counted it last. lastFile stops a code that is repeated inside one file
	// from counting twice.
	type tally struct{ files, lastFile int32 }
	tallies := make(map[string]tally)

	for i, rec := range m.Files {
		fileNo := int32(i + 1) // 0 means "not counted by any file yet"
		err := forEachLine(in.shardPath(rec.ID, bucket), func(code string) {
			t := tallies[code]
			if t.lastFile == fileNo {
				return // repeated inside this file
			}
			t.files++
			t.lastFile = fileNo
			tallies[code] = t
		})
		if err != nil {
			return nil, err
		}
	}

	var valid []string
	for code, t := range tallies {
		if int(t.files) >= in.cfg.Rule.MinFiles {
			valid = append(valid, code)
		}
	}
	sort.Strings(valid)
	return valid, nil
}

// forEachLine calls fn for every line of the file at path.
func forEachLine(path string, fn func(line string)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(bufio.NewReaderSize(f, 1<<20))
	for scanner.Scan() {
		fn(scanner.Text())
	}
	return scanner.Err()
}
