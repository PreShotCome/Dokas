package billing

import (
	"encoding/json"
	"io"
)

// jsonDecode is a tiny shim so stripe.go can take an interface that's
// satisfied by both *bytes.Buffer (tests) and *http.Response.Body.
func jsonDecode(r interface{ Read([]byte) (int, error) }, into any) error {
	return json.NewDecoder(r.(io.Reader)).Decode(into)
}
