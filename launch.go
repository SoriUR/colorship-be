package main

import (
	"encoding/json"
	"net/http"
)

// launchHandler получает пользователя по токену и возвращает user_id, free_messages_left, paid_messages_left, is_using_paid.
func launchHandler(w http.ResponseWriter, r *http.Request) {
	userID, err := getUserIDFromRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	var resp struct {
		UserID           string `json:"user_id"`
		FreeMessagesLeft int    `json:"free_messages_left"`
		PaidMessagesLeft int    `json:"paid_messages_left"`
		IsUsingPaid      bool   `json:"is_using_paid"`
	}
	resp.UserID = userID

	err = db.QueryRow(`
		select free_messages_left, paid_messages_left, is_using_paid
		from user_credits
		where user_id = $1
	`, userID).Scan(&resp.FreeMessagesLeft, &resp.PaidMessagesLeft, &resp.IsUsingPaid)
	if err != nil {
		http.Error(w, "User not found or database error", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
