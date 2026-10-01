package ingestion

type webAdapter struct{}

func (webAdapter) Kind() string { return "web" }

// Parse returns the page body as-is; selector extraction happens during
// normalization.
func (webAdapter) Parse(body []byte) (Result, error) {
	return Result{Body: append([]byte(nil), body...)}, nil
}
