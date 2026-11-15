package setup

import "testing"

func TestSafeLogFieldStripsControlCharacters(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"plain snowflake id", "1111111111111111111", "1111111111111111111"},
		{"newline log forgery", "111\n2026/11/16 INFO forged", "1112026/11/16 INFO forged"},
		{"carriage return and tab", "a\rb\tc", "abc"},
		{"delete char", "a\x7fb", "ab"},
		{"japanese text kept", "カテゴリ", "カテゴリ"},
	}
	for _, tc := range tests {
		if got := safeLogField(tc.in); got != tc.want {
			t.Errorf("%s: safeLogField(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}
