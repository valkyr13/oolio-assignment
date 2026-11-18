package coupon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "valid.txt")
	os.WriteFile(path, []byte("HAPPYHRS\n\nFIFTYOFF\n"), 0o644)

	s, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Len() != 2 {
		t.Fatalf("loaded %d codes, want 2 (blank lines ignored)", s.Len())
	}
	tests := []struct {
		code string
		want bool
	}{
		{"HAPPYHRS", true},
		{"FIFTYOFF", true},
		{"happyhrs", false}, // exact match, case included
		{"SUPER100", false},
		{"", false},
	}
	for _, tt := range tests {
		got, err := s.Contains(context.Background(), tt.code)
		if err != nil || got != tt.want {
			t.Errorf("Contains(%q) = %v, %v; want %v, nil", tt.code, got, err, tt.want)
		}
	}
}

func TestLoadFileMissing(t *testing.T) {
	if _, err := LoadFile(filepath.Join(t.TempDir(), "nope.txt")); err == nil {
		t.Fatal("loading a missing file should fail")
	}
}
