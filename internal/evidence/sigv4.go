package evidence

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sort"
	"strings"
	"time"
)

// emptyPayloadHash is SHA-256 of an empty body — used to sign GET/DELETE.
const emptyPayloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

// signV4 adds an AWS Signature Version 4 Authorization header to req. It
// signs the Host header plus every x-amz-* header already set on the
// request; payloadHash is the hex SHA-256 of the body. This is the standard
// algorithm and works against AWS S3, Cloudflare R2, and MinIO.
func signV4(req *http.Request, payloadHash, accessKey, secretKey, region, service string, t time.Time) {
	amzDate := t.UTC().Format("20060102T150405Z")
	dateStamp := t.UTC().Format("20060102")
	req.Header.Set("X-Amz-Date", amzDate)

	host := req.Host
	if host == "" {
		host = req.URL.Host
	}

	// The signed header set: host + every x-amz-* header on the request.
	type hdr struct{ name, value string }
	signed := []hdr{{"host", host}}
	for name, vals := range req.Header {
		if ln := strings.ToLower(name); strings.HasPrefix(ln, "x-amz-") {
			signed = append(signed, hdr{ln, strings.TrimSpace(strings.Join(vals, ","))})
		}
	}
	sort.Slice(signed, func(i, j int) bool { return signed[i].name < signed[j].name })

	var canonHeaders strings.Builder
	names := make([]string, len(signed))
	for i, h := range signed {
		canonHeaders.WriteString(h.name)
		canonHeaders.WriteByte(':')
		canonHeaders.WriteString(h.value)
		canonHeaders.WriteByte('\n')
		names[i] = h.name
	}
	signedHeaders := strings.Join(names, ";")

	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI(req.URL.Path),
		req.URL.RawQuery, // our object requests carry no query string
		canonHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := dateStamp + "/" + region + "/" + service + "/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	kDate := hmacSHA256([]byte("AWS4"+secretKey), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	kSigning := hmacSHA256(kService, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(kSigning, stringToSign))

	req.Header.Set("Authorization",
		"AWS4-HMAC-SHA256 Credential="+accessKey+"/"+scope+
			", SignedHeaders="+signedHeaders+
			", Signature="+signature)
}

// canonicalURI URI-encodes each path segment per RFC 3986, keeping '/'.
func canonicalURI(path string) string {
	if path == "" {
		return "/"
	}
	segs := strings.Split(path, "/")
	for i, s := range segs {
		segs[i] = rfc3986Escape(s)
	}
	return strings.Join(segs, "/")
}

// rfc3986Escape percent-encodes everything outside the RFC 3986 unreserved
// set, which is how SigV4 expects path segments encoded.
func rfc3986Escape(s string) string {
	const upperhex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(upperhex[c>>4])
			b.WriteByte(upperhex[c&0x0f])
		}
	}
	return b.String()
}
