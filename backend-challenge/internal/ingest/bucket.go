package ingest

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// FNV-1a, 64-bit: the same algorithm as hash/fnv's New64a, written inline so
// hashing hundreds of millions of codes allocates nothing.
//
// The hash must be stable: the same code has to land in the same bucket on
// every run and every machine, or shards written today would not line up with
// shards written later. hash/maphash cannot be used because its seed is random
// in every process.
const (
	fnvOffset64 = 14695981039346656037
	fnvPrime64  = 1099511628211
)

// bucketOf returns the bucket for code. The bucket count is a power of two and
// mask is count-1, so the bucket is the low bits of the hash. That makes the
// count cheap to grow later: with twice as many buckets, bucket b splits into
// exactly b and b+count, so existing shards can be split without re-reading
// the source files.
func bucketOf(code string, mask uint64) int {
	h := uint64(fnvOffset64)
	for i := 0; i < len(code); i++ {
		h ^= uint64(code[i])
		h *= fnvPrime64
	}
	return int(h & mask)
}

func bucketName(b int) string { return fmt.Sprintf("bucket-%04x", b) }

// bucketFiles holds one open, buffered file per bucket while a source file is
// being sharded. Memory use is the buffers only (count x 64 KB), whatever the
// size of the source file.
type bucketFiles struct {
	mask    uint64
	files   []*os.File
	writers []*bufio.Writer
}

func createBucketFiles(dir string, count int) (*bucketFiles, error) {
	b := &bucketFiles{
		mask:    uint64(count - 1),
		files:   make([]*os.File, count),
		writers: make([]*bufio.Writer, count),
	}
	for i := 0; i < count; i++ {
		f, err := os.Create(filepath.Join(dir, bucketName(i)))
		if err != nil {
			b.close()
			return nil, err
		}
		b.files[i] = f
		b.writers[i] = bufio.NewWriterSize(f, 64<<10)
	}
	return b, nil
}

// write appends code, one per line, to the bucket its hash selects.
func (b *bucketFiles) write(code string) error {
	w := b.writers[bucketOf(code, b.mask)]
	if _, err := w.WriteString(code); err != nil {
		return err
	}
	return w.WriteByte('\n')
}

// close flushes every bucket to disk and closes it. It is safe to call more
// than once; later calls do nothing.
func (b *bucketFiles) close() error {
	var errs []error
	for i, f := range b.files {
		if f == nil {
			continue
		}
		if err := b.writers[i].Flush(); err != nil {
			errs = append(errs, err)
		}
		// Sync before the shards are committed, so a power cut cannot leave a
		// committed file with half-written shards.
		if err := f.Sync(); err != nil {
			errs = append(errs, err)
		}
		if err := f.Close(); err != nil {
			errs = append(errs, err)
		}
		b.files[i] = nil
	}
	return errors.Join(errs...)
}
