package auth

import (
	"net/http"
)

func Middleware(v *Validator, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" || r.URL.Path == "/health/" {
			next.ServeHTTP(w, r)
			return
		}

		tokenStr := extractToken(r)
		if tokenStr == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		if err := v.Validate(tokenStr); err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func extractToken(r *http.Request) string {
	if h := r.Header.Get("Cf-Access-Jwt-Assertion"); h != "" {
		return h
	}
	if c, err := r.Cookie("CF_Authorization"); err == nil {
		return c.Value
	}
	return ""
}
