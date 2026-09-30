package cors

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const (
	allowedHeaders = "Content-Type, Idempotency-Key"
	allowedMethods = "GET, POST, PUT, DELETE, OPTIONS"
	exposedHeaders = "Idempotency-Replayed"
)

// Middleware allows credentialed browser requests only from configured
// application origins. Origin checks do not replace authentication or CSRF
// protection on state-changing endpoints.
func Middleware(allowedOrigins []string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowed[origin] = struct{}{}
	}

	return func(context *gin.Context) {
		origin := context.GetHeader("Origin")
		_, originAllowed := allowed[origin]
		if originAllowed {
			context.Header("Access-Control-Allow-Origin", origin)
			context.Header("Access-Control-Allow-Credentials", "true")
			context.Header("Access-Control-Allow-Headers", allowedHeaders)
			context.Header("Access-Control-Allow-Methods", allowedMethods)
			context.Header("Access-Control-Expose-Headers", exposedHeaders)
			context.Header("Vary", "Origin")
		}

		if context.Request.Method != http.MethodOptions {
			context.Next()
			return
		}
		if !originAllowed {
			context.AbortWithStatus(http.StatusForbidden)
			return
		}
		context.AbortWithStatus(http.StatusNoContent)
	}
}
