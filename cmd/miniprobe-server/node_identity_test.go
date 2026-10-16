package main

import (
	"testing"
)

func TestASNNameOnlyParsesPublishedDescription(t *testing.T) {
	if got := parseASNNameRecord("13335 | US | arin | 2010-07-14 | CLOUDFLARENET, US", "13335"); got != "CLOUDFLARENET, US" {
		t.Fatalf("ASN name got %q", got)
	}
	if got := parseASNNameRecord("13335 | US | arin | 2010-07-14 | CLOUDFLARENET, US", "16509"); got != "" {
		t.Fatalf("must not mix different ASNs: %q", got)
	}
	if got := parseASNNameRecord("garbled", "13335"); got != "" {
		t.Fatalf("invalid record should fail closed: %q", got)
	}
}
