package coupon

import "testing"

func TestLengthOK(t *testing.T) {
	r := DefaultRule
	tests := []struct {
		code string
		want bool
	}{
		{"", false},
		{"AAAAAAA", false},     // 7: one too short
		{"AAAAAAAA", true},     // 8: shortest allowed
		{"AAAAAAAAA", true},    // 9
		{"AAAAAAAAAA", true},   // 10: longest allowed
		{"AAAAAAAAAAA", false}, // 11: one too long
		{"ÄÄÄÄÄÄÄÄ", true},     // 8 characters but 16 bytes: characters count
	}
	for _, tt := range tests {
		if got := r.LengthOK(tt.code); got != tt.want {
			t.Errorf("LengthOK(%q) = %v, want %v", tt.code, got, tt.want)
		}
	}
}

func TestRuleValidate(t *testing.T) {
	tests := []struct {
		name    string
		rule    Rule
		wantErr bool
	}{
		{"default", DefaultRule, false},
		{"zero min length", Rule{MinLen: 0, MaxLen: 10, MinFiles: 2}, true},
		{"max below min", Rule{MinLen: 10, MaxLen: 8, MinFiles: 2}, true},
		{"zero min files", Rule{MinLen: 8, MaxLen: 10, MinFiles: 0}, true},
		{"single length", Rule{MinLen: 8, MaxLen: 8, MinFiles: 1}, false},
	}
	for _, tt := range tests {
		err := tt.rule.Validate()
		if (err != nil) != tt.wantErr {
			t.Errorf("%s: Validate() error = %v, wantErr %v", tt.name, err, tt.wantErr)
		}
	}
}
