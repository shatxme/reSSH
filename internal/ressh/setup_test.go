package ressh

import "testing"

func TestSafeFilePart(t *testing.T) {
	cases := map[string]string{
		"1.2.3.4":            "1.2.3.4",
		"2001:db8::1":        "2001_db8__1",
		"root@example.com":   "root_example.com",
		" /bad\\name:host/ ": "bad_name_host",
		"***":                "host",
	}
	for input, want := range cases {
		if got := safeFilePart(input); got != want {
			t.Fatalf("safeFilePart(%q) = %q, want %q", input, got, want)
		}
	}
}
