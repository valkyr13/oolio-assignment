package auth

import (
	"context"
	"testing"
)

func TestParseKeysAndLookup(t *testing.T) {
	keys, err := ParseKeys("apitest:create_order, readonly: ,ops:create_order+refund")
	if err != nil {
		t.Fatal(err)
	}
	if keys.Len() != 3 {
		t.Fatalf("parsed %d keys, want 3", keys.Len())
	}
	tests := []struct {
		secret    string
		found     bool
		name      string
		canOrder  bool
		canRefund bool
	}{
		{"apitest", true, "key-1", true, false},
		{"readonly", true, "key-2", false, false},
		{"ops", true, "key-3", true, true},
		{"APITEST", false, "", false, false}, // secrets are exact
		{"", false, "", false, false},
		{"apites", false, "", false, false},
	}
	for _, tt := range tests {
		key, ok, err := keys.Lookup(context.Background(), tt.secret)
		if err != nil || ok != tt.found || key.Name != tt.name ||
			key.Has(ScopeCreateOrder) != tt.canOrder || key.Has("refund") != tt.canRefund {
			t.Errorf("Lookup(%q) = %+v, %v, %v", tt.secret, key, ok, err)
		}
	}
}

func TestParseKeysRejectsBadConfig(t *testing.T) {
	for _, config := range []string{
		"no-colon",
		":create_order",           // empty secret
		"same:create_order,same:", // duplicate secret
	} {
		if _, err := ParseKeys(config); err == nil {
			t.Errorf("ParseKeys(%q) should fail", config)
		}
	}
}

func TestNoKeysConfigured(t *testing.T) {
	keys, err := ParseKeys("   ")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := keys.Lookup(context.Background(), "anything"); ok {
		t.Fatal("with no keys configured, no key should match")
	}
}
