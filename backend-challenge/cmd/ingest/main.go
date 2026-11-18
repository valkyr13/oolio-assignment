// Command ingest builds the list of valid coupon codes from raw coupon files.
//
//	go run ./cmd/ingest -state ~/oolio-data/state couponbase1.gz couponbase2.gz couponbase3.gz
//
// Files that were ingested before are recognised by their contents and
// skipped, so it is safe to pass the full list every time: adding a fourth
// file only reads the fourth file. The valid list is then rebuilt from the
// stored shards and published atomically to -out. With no files, it only
// rebuilds, for example after changing -min-files.
//
// If any file fails, nothing is published and the previous list stays live.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/valkyr13/oolio-assignment/internal/coupon"
	"github.com/valkyr13/oolio-assignment/internal/ingest"
)

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		log.Fatalf("ingest: %v", err)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("ingest", flag.ContinueOnError)
	stateDir := fs.String("state", "state", "directory for the manifest and shards (large; keep it out of git)")
	out := fs.String("out", "data/valid_coupons.txt", "where to publish the valid coupon codes")
	buckets := fs.Int("buckets", ingest.DefaultBuckets, "hash buckets, a power of two: more buckets, less memory")
	minLen := fs.Int("min-len", coupon.DefaultRule.MinLen, "shortest valid code, in characters")
	maxLen := fs.Int("max-len", coupon.DefaultRule.MaxLen, "longest valid code, in characters")
	minFiles := fs.Int("min-files", coupon.DefaultRule.MinFiles, "how many different files a code must appear in")
	parallel := fs.Int("parallel", 3, "files to shard at the same time; each opens -buckets files")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: ingest [flags] [coupon-file.gz ...]\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	in, err := ingest.Open(ingest.Config{
		StateDir: *stateDir,
		Buckets:  *buckets,
		Rule:     coupon.Rule{MinLen: *minLen, MaxLen: *maxLen, MinFiles: *minFiles},
	})
	if err != nil {
		return err
	}

	start := time.Now()
	results, err := in.AddFiles(fs.Args(), *parallel)
	for _, r := range results {
		switch {
		case r.File.ID == "":
			// failed; reported in err below
		case r.Added:
			log.Printf("added    %s: %d codes kept, %d lines skipped (%s)",
				r.File.Name, r.File.Codes, r.File.Skipped, r.Took.Round(time.Millisecond))
		default:
			log.Printf("skipped  %s: already ingested", r.File.Name)
		}
	}
	if err != nil {
		return fmt.Errorf("%w\nnothing was published; the previous list is unchanged", err)
	}

	stats, err := in.Build(*out)
	if err != nil {
		return fmt.Errorf("building the valid list: %w\nthe previous list is unchanged", err)
	}
	log.Printf("published %d valid codes from %d files to %s (combine %s, total %s)",
		stats.Valid, stats.Files, *out, stats.Took.Round(time.Millisecond), time.Since(start).Round(time.Millisecond))
	return nil
}
