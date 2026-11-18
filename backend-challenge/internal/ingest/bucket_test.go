package ingest

import (
	"hash/fnv"
	"math/rand"
	"testing"
)

const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func randomCodes(n int) []string {
	r := rand.New(rand.NewSource(1))
	codes := make([]string, n)
	b := make([]byte, 8)
	for i := range codes {
		for j := range b {
			b[j] = alphabet[r.Intn(len(alphabet))]
		}
		codes[i] = string(b)
	}
	return codes
}

// The inline hash must be standard FNV-1a, so the bucket layout is documented
// and reproducible by any other tool.
func TestBucketOfIsFNV1a(t *testing.T) {
	for _, code := range randomCodes(1000) {
		h := fnv.New64a()
		h.Write([]byte(code))
		if got, want := bucketOf(code, 255), int(h.Sum64()&255); got != want {
			t.Fatalf("bucketOf(%q) = %d, want %d", code, got, want)
		}
	}
}

// Doubling the bucket count must split bucket b into exactly b and b+count,
// so stored shards can be split without reading the source files again.
func TestDoublingSplitsEachBucketInTwo(t *testing.T) {
	const count = 256
	for _, code := range randomCodes(10000) {
		b := bucketOf(code, count-1)
		b2 := bucketOf(code, 2*count-1)
		if b2 != b && b2 != b+count {
			t.Fatalf("%q: bucket %d of %d became %d of %d", code, b, count, b2, 2*count)
		}
	}
}

// Memory is bounded by the largest bucket, so codes must spread evenly.
func TestBucketsAreEven(t *testing.T) {
	const count, n = 256, 200000
	sizes := make([]int, count)
	for _, code := range randomCodes(n) {
		sizes[bucketOf(code, count-1)]++
	}
	mean := n / count
	for b, size := range sizes {
		if size < mean*3/4 || size > mean*5/4 {
			t.Fatalf("bucket %d holds %d codes; mean is %d", b, size, mean)
		}
	}
}

func BenchmarkBucketOf(b *testing.B) {
	codes := randomCodes(1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bucketOf(codes[i&1023], 255)
	}
}
