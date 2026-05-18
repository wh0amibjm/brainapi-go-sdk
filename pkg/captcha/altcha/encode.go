package altcha

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// Encode produces the base64-of-JSON payload to drop into auxiliary.captcha
// on POST /users. The JSON field order doesn't matter to BRAIN's verifier —
// it parses the payload back into a struct on the server side.
func Encode(s *Solution) (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("altcha: marshal solution: %w", err)
	}
	return base64.StdEncoding.EncodeToString(b), nil
}
