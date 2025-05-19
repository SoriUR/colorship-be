package main

import (
	"encoding/json"
	"net/http"
)

type ErrorResponse struct {
	ErrorType   string      `json:"error_type"`
	Description string      `json:"description"`
	Payload     interface{} `json:"payload,omitempty"`
}

func writeError(w http.ResponseWriter, errorType, description string, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	json.NewEncoder(w).Encode(ErrorResponse{
		ErrorType:   errorType,
		Description: description,
		Payload:     payload,
	})
}
