package util

import "testing"

func TestMaskSensitiveHeaderValuePreferredAuth(t *testing.T) {
	for _, name := range []string{"X-CLIProxy-Preferred-Auth", "x-cliproxy-preferred-auth", " X-CLIPROXY-PREFERRED-AUTH "} {
		for _, value := range []string{"iauser02@example.test", "untrusted\r\nlog content", ""} {
			if got := MaskSensitiveHeaderValue(name, value); got != "[REDACTED]" {
				t.Fatal("preferred auth header was not fully redacted")
			}
		}
	}
	if got := MaskSensitiveHeaderValue("X-Request-ID", "request-1"); got != "request-1" {
		t.Fatal("unrelated header masking changed")
	}
}
