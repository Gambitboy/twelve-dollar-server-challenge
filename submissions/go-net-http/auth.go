package main

import (
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type user struct {
	id       int64
	username string
}

type authedHandler func(w http.ResponseWriter, r *http.Request, u user)

// requireAuth verifies `Authorization: Bearer <jwt>` (HS256, exp checked) and passes the
// token's user to next, or responds 401.
func (a *app) requireAuth(next authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		tokenString := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))

		claims := jwt.MapClaims{}
		_, err := jwt.ParseWithClaims(tokenString, claims, func(*jwt.Token) (any, error) {
			return a.jwtSecret, nil
		}, jwt.WithValidMethods([]string{"HS256"}))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		id, okID := userID(claims["sub"])
		username, okName := claims["username"].(string)
		if !okID || !okName {
			writeError(w, http.StatusUnauthorized, "invalid token payload")
			return
		}
		next(w, r, user{id: id, username: username})
	}
}

// userID mirrors the reference's Number(payload.sub): a positive integer, normally a string.
func userID(sub any) (int64, bool) {
	switch v := sub.(type) {
	case string:
		id, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return id, err == nil && id > 0
	case float64:
		return int64(v), v > 0 && v == math.Trunc(v) && v <= 1<<53
	}
	return 0, false
}
