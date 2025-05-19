package main

import (
	"fmt"
	"net/http"
	"strings"
)

func getUserIDFromRequest(r *http.Request) (string, error) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return "", fmt.Errorf("missing Authorization header")
	}
	token := strings.TrimPrefix(auth, "Bearer ")
	if token == "" {
		return "", fmt.Errorf("invalid Authorization token")
	}

	var userID string
	err := db.QueryRow(`SELECT id FROM users WHERE access_token = $1`, token).Scan(&userID)
	if err != nil {
		return "", fmt.Errorf("invalid token")
	}
	return userID, nil
}
