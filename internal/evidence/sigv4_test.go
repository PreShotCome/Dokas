package evidence

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestSignV4Vanilla checks signV4 against the canonical "get-vanilla" case
// from AWS's published SigV4 test suite — a known request that must produce
// a known signature.
func TestSignV4Vanilla(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://example.amazonaws.com/", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	signV4(req, emptyPayloadHash,
		"AKIDEXAMPLE", "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
		"us-east-1", "service",
		time.Date(2015, 8, 30, 12, 36, 0, 0, time.UTC))

	auth := req.Header.Get("Authorization")
	const wantSig = "Signature=5fa00fa31553b73ebf1942676e86291e8372ff2a2260956d9b8aae1d763fbf31"
	if !strings.Contains(auth, wantSig) {
		t.Fatalf("Authorization = %q\nwant it to contain %q", auth, wantSig)
	}
	if !strings.Contains(auth, "SignedHeaders=host;x-amz-date") {
		t.Errorf("Authorization = %q\nwant SignedHeaders=host;x-amz-date", auth)
	}
	if !strings.Contains(auth, "Credential=AKIDEXAMPLE/20150830/us-east-1/service/aws4_request") {
		t.Errorf("Authorization = %q\nwant the expected credential scope", auth)
	}
}

func TestCanonicalURI(t *testing.T) {
	cases := map[string]string{
		"":          "/",
		"/":         "/",
		"/3b9f.pdf": "/3b9f.pdf",
		"/a b/c":    "/a%20b/c",
	}
	for in, want := range cases {
		if got := canonicalURI(in); got != want {
			t.Errorf("canonicalURI(%q) = %q, want %q", in, got, want)
		}
	}
}
