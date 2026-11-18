// Package ingest turns raw coupon files into the list of valid coupon codes.
//
// It works in two stages, so memory stays small however large the files are:
//
//  1. Shard (AddFiles). Each new file is decompressed and parsed once, ever.
//     Every line is taken exactly as written, checked for length, and
//     appended to one of N bucket files picked by a hash of the code. The same code always lands
//     in the same bucket number, whichever file it came from. A file's shards
//     are kept, so the file never has to be read again.
//
//  2. Combine (Build). For each bucket, count how many different files each
//     code appears in, using the shards of every ingested file. Only one
//     bucket is held in memory at a time. Codes found in at least
//     Rule.MinFiles files are published.
//
// A manifest records which files have been ingested, identified by a SHA-256
// fingerprint of their contents, so a file added twice (even under another
// name) is counted once.
//
// Only one ingest should run against a state directory at a time.
package ingest

import (
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/valkyr13/oolio-assignment/internal/coupon"
)

const (
	// DefaultBuckets keeps one bucket of 313 million codes at about
	// 1.2 million codes, a map of roughly 100 MB.
	DefaultBuckets = 256
	maxBuckets     = 1 << 16 // bucket file names have 4 hex digits
)

// Config controls an Ingester.
type Config struct {
	StateDir string      // holds the manifest and shards; not committed to git
	Buckets  int         // number of hash buckets; must be a power of two
	Rule     coupon.Rule // length rule applied while sharding, MinFiles at Build
}

// Ingester shards coupon files into a state directory and builds the list of
// valid codes from it.
//
// State directory layout:
//
//	manifest.json          which files have been ingested
//	shards/<id>/bucket-*   one folder per ingested file, one file per bucket
//	staging/               scratch space while a file is being sharded
type Ingester struct {
	cfg Config

	mu       sync.Mutex // guards manifest
	manifest Manifest
}

// Open prepares the state directory, creating it if needed. Existing state
// must have been built with the same bucket count and length rule.
func Open(cfg Config) (*Ingester, error) {
	if err := cfg.Rule.Validate(); err != nil {
		return nil, err
	}
	if cfg.Buckets < 1 || cfg.Buckets > maxBuckets || cfg.Buckets&(cfg.Buckets-1) != 0 {
		return nil, fmt.Errorf("buckets must be a power of two between 1 and %d, got %d", maxBuckets, cfg.Buckets)
	}
	in := &Ingester{cfg: cfg}

	// Anything in staging is from a run that stopped before committing, so it
	// was never counted and is always safe to delete.
	if err := os.RemoveAll(in.stagingDir()); err != nil {
		return nil, err
	}
	for _, dir := range []string{in.stagingDir(), in.shardsDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}

	m, err := loadManifest(in.manifestPath())
	switch {
	case errors.Is(err, fs.ErrNotExist):
		m = Manifest{
			Format:  manifestFormat,
			Buckets: cfg.Buckets,
			MinLen:  cfg.Rule.MinLen,
			MaxLen:  cfg.Rule.MaxLen,
		}
	case err != nil:
		return nil, err
	default:
		if err := m.compatible(cfg); err != nil {
			return nil, err
		}
	}
	in.manifest = m
	return in, nil
}

// Files returns the files ingested so far.
func (in *Ingester) Files() []FileRecord {
	in.mu.Lock()
	defer in.mu.Unlock()
	return append([]FileRecord(nil), in.manifest.Files...)
}

// AddResult reports what happened to one file passed to AddFiles.
type AddResult struct {
	File  FileRecord
	Added bool // false if a file with the same contents was already ingested
	Took  time.Duration
}

// AddFiles shards every file that has not been ingested before, running up to
// parallel files at the same time. Files are independent, so they shard in
// parallel without locking; gzip decompression uses one CPU core per file.
//
// Results come back in the order of paths. If any file fails, the error
// lists every failure; callers should then skip Build, so the previously
// published list stays live.
func (in *Ingester) AddFiles(paths []string, parallel int) ([]AddResult, error) {
	if parallel < 1 {
		parallel = 1
	}
	results := make([]AddResult, len(paths))
	errs := make([]error, len(paths))
	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	for i, path := range paths {
		wg.Add(1)
		go func(i int, path string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i], errs[i] = in.addFile(path)
			if errs[i] != nil {
				errs[i] = fmt.Errorf("%s: %w", path, errs[i])
			}
		}(i, path)
	}
	wg.Wait()
	return results, errors.Join(errs...)
}

func (in *Ingester) addFile(path string) (AddResult, error) {
	start := time.Now()

	// Step 1: fingerprint the raw bytes. This is cheap (about a second for a
	// 700 MB file) compared with decompressing and parsing it, so re-running
	// ingest with the full list of files skips known files almost instantly.
	id, size, err := fingerprint(path)
	if err != nil {
		return AddResult{}, err
	}
	rec := FileRecord{ID: id, Name: filepath.Base(path), Size: size}
	if in.known(id) {
		return AddResult{File: rec, Took: time.Since(start)}, nil
	}

	// Step 2: shard into a staging folder. Until commit moves the folder into
	// shards/, it is scratch space and is removed on every exit path.
	staging, err := os.MkdirTemp(in.stagingDir(), "file-")
	if err != nil {
		return AddResult{}, err
	}
	defer os.RemoveAll(staging)

	codes, skipped, err := in.shard(path, staging)
	if err != nil {
		return AddResult{}, err
	}
	rec.Codes, rec.Skipped = codes, skipped
	rec.AddedAt = time.Now().UTC()

	// Step 3: commit, so the file counts from now on.
	added, err := in.commit(staging, rec)
	if err != nil {
		return AddResult{}, err
	}
	return AddResult{File: rec, Added: added, Took: time.Since(start)}, nil
}

// fingerprint returns the SHA-256 of the file's bytes and its size.
func fingerprint(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// shard reads a gzip file line by line and writes each code that passes the
// length rule to its bucket file in dir. It returns how many codes it kept and
// how many lines it skipped.
func (in *Ingester) shard(path, dir string) (codes, skipped int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(bufio.NewReaderSize(f, 1<<20))
	if err != nil {
		return 0, 0, fmt.Errorf("not a gzip file: %w", err)
	}
	defer gz.Close()

	buckets, err := createBucketFiles(dir, in.cfg.Buckets)
	if err != nil {
		return 0, 0, err
	}
	defer buckets.close() // on error paths; the success path closes explicitly

	// Each line is taken exactly as written. The line reader removes the line
	// ending, including a Windows "\r\n"; nothing else is changed, because
	// cleaning codes up would change which codes match across files. Lines of
	// the wrong length are skipped and counted, so a bad file shows up in the
	// manifest instead of being quietly repaired.
	scanner := bufio.NewScanner(gz)
	for scanner.Scan() {
		code := scanner.Text()
		if !in.cfg.Rule.LengthOK(code) {
			skipped++
			continue
		}
		if err := buckets.write(code); err != nil {
			return 0, 0, err
		}
		codes++
	}
	if err := scanner.Err(); err != nil {
		// A truncated or corrupt file stops here, before anything is committed.
		return 0, 0, fmt.Errorf("reading: %w", err)
	}
	if err := buckets.close(); err != nil {
		return 0, 0, err
	}
	return codes, skipped, nil
}

func (in *Ingester) known(id string) bool {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.manifest.has(id)
}

// commit moves a fully written staging folder into shards/ and records the
// file in the manifest. The manifest is written last: until it is saved, the
// shards are not counted, so a crash in between only leaves an unused folder.
func (in *Ingester) commit(staging string, rec FileRecord) (bool, error) {
	in.mu.Lock()
	defer in.mu.Unlock()

	// Two copies of the same file may have been sharded in parallel; the
	// second one to finish is a duplicate.
	if in.manifest.has(rec.ID) {
		return false, nil
	}

	final := in.shardDir(rec.ID)
	// A folder can only be here if a previous run crashed after the rename but
	// before saving the manifest; it was never counted, so replace it.
	if err := os.RemoveAll(final); err != nil {
		return false, err
	}
	if err := os.Rename(staging, final); err != nil {
		return false, err
	}

	next := in.manifest
	next.Files = append(append([]FileRecord(nil), in.manifest.Files...), rec)
	if err := saveManifest(in.manifestPath(), next); err != nil {
		return false, err
	}
	in.manifest = next
	return true, nil
}

func (in *Ingester) manifestPath() string { return filepath.Join(in.cfg.StateDir, "manifest.json") }
func (in *Ingester) shardsDir() string    { return filepath.Join(in.cfg.StateDir, "shards") }
func (in *Ingester) stagingDir() string   { return filepath.Join(in.cfg.StateDir, "staging") }
func (in *Ingester) shardDir(id string) string {
	return filepath.Join(in.shardsDir(), id)
}
func (in *Ingester) shardPath(id string, bucket int) string {
	return filepath.Join(in.shardDir(id), bucketName(bucket))
}
