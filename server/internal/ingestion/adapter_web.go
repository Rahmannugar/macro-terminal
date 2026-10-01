package ingestion

type webAdapter struct{}

func (webAdapter) Kind() string { return "web" }

// Parse returns the page unchanged; pulling fields out of the HTML happens
// in normalization.
func (webAdapter) Parse(body []byte) (Result, error) {
	return Result{Body: append([]byte(nil), body...)}, nil
}
