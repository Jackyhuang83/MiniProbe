package main

import (
	"testing"
)

func TestEgressIdentityInputSafety(t *testing.T) {
	cases := []struct{ raw string; bits int; valid bool }{
		{"8.8.8.8", 4, true},
		{"2001:4860:4860::8888", 6, true},
		{"127.0.0.1", 4, false},
		{"10.0.0.1", 4, false},
		{"100.64.0.1", 4, false},
		{"8.8.8.8", 6, false},
		{"bad-ip", 4, false},
	}
	for _, tc := range cases {
		result := validatedEgressIP(tc.raw, tc.bits)
		if (result != "") != tc.valid { t.Fatalf("%q family %d -> %q", tc.raw, tc.bits, result) }
	}
}

func TestParseCymruASName(t *testing.T) {
	got := parseCymruASName("15169 | US | arin | 2000-03-30 | GOOGLE, US")
	if got != "GOOGLE, US" { t.Fatalf("name=%q", got) }
	if parseCymruASName("invalid") != "" { t.Fatal("unverified ASN name") }
}
