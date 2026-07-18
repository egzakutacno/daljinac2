package auth

import (
	"crypto/subtle"
	"net/http"
)

const SharedSecret = "234d130007706cd69359c94b89d3dd70"

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if token == "" {
			http.Error(w, `{"error":"missing authorization"}`, http.StatusUnauthorized)
			return
		}
		if len(token) < 7 || token[:7] != "Bearer " {
			http.Error(w, `{"error":"invalid authorization format"}`, http.StatusUnauthorized)
			return
		}
		given := token[7:]
		if subtle.ConstantTimeCompare([]byte(given), []byte(SharedSecret)) != 1 {
			http.Error(w, `{"error":"invalid token"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
