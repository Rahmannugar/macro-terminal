package ingestion

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type apiAdapter struct{}

func (apiAdapter) Kind() string { return "api" }

// Parse accepts JSON and XML (SDMX statistics services return XML) and
// rejects anything else as malformed. Decoding the body into records
// belongs to normalization.
func (apiAdapter) Parse(body []byte) (Result, error) {
	trimmed := bytes.TrimSpace(body)
	if !json.Valid(body) && !bytes.HasPrefix(trimmed, []byte("<?xml")) {
		return Result{}, fmt.Errorf("%w: response is neither JSON nor XML", ErrMalformed)
	}
	return Result{Body: append([]byte(nil), body...)}, nil
}
