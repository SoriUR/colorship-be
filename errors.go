package main

import (
	"encoding/json"
	"log"
	"net/http"
)

type ErrorResponse struct {
	ErrorType   string      `json:"error_type"`
	Description string      `json:"description"`
	Payload     interface{} `json:"payload,omitempty"`
}

func writeError(w http.ResponseWriter, errorType, description string, payload interface{}, err error) {
	if err != nil {
		log.Printf("ERROR [%s]: %s | %v", errorType, description, err)
	} else {
		log.Printf("ERROR [%s]: %s", errorType, description)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	json.NewEncoder(w).Encode(ErrorResponse{
		ErrorType:   errorType,
		Description: description,
		Payload:     payload,
	})
}
