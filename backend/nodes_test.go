package main

import "testing"

func TestParseClientUA(t *testing.T) {
	for ua, want := range map[string][2]string{
		"NavalRelay-CLI/1.6.0": {"cli", "1.6.0"}, "NavalRelay/1.5.0": {"app", "1.5.0"}, "curl/8": {"", ""}, "": {"", ""},
	} {
		k, v := parseClientUA(ua)
		if k != want[0] || v != want[1] {
			t.Errorf("%q: %s %s", ua, k, v)
		}
	}
}

func TestVerLess(t *testing.T) {
	if !verLess("1.5.0", "1.6.0") || verLess("1.6.0", "1.6.0") || verLess("1.10.0", "1.9.9") || !verLess("1.6.0-dev", "1.6.1") {
		t.Error("comparação de versões incorreta")
	}
}
