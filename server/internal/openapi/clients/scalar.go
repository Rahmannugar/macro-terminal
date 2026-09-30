package clients

import "fmt"

// ScalarHTML keeps the replaceable renderer outside the OpenAPI contract.
func ScalarHTML(documentURL string) string {
	return fmt.Sprintf(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Macro Terminal API Reference</title></head>
<body><div id="app"></div><script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script><script>Scalar.createApiReference('#app',{url:%q,theme:'default'})</script></body>
</html>`, documentURL)
}
