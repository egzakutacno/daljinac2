package auth

import (
	"crypto/subtle"
	"net/http"
)

const SharedSecret = "916de2678b4319090a640799f7ca7a6e"

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
