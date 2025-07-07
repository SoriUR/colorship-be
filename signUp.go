package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/google/uuid"
)

type signUpResponse struct {
	AccessToken string `json:"access_token"`
}

func signUpHandler(w http.ResponseWriter, r *http.Request) {
	userID := uuid.New().String()
	token := uuid.New().String()

	_, err := db.Exec(`insert into users (id, access_token, created_at) values ($1, $2, now())`, userID, token)
	if err != nil {
		http.Error(w, "failed to create user", http.StatusInternalServerError)
		log.Println("create user error:", err)
		return
	}

	_, err = db.Exec(`insert into user_credits (user_id, count) values ($1, 5)`, userID)
	if err != nil {
		http.Error(w, "failed to initialize user credits", http.StatusInternalServerError)
		log.Println("create user_credits error:", err)
		return
	}

	resp := signUpResponse{AccessToken: token}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
