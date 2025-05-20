package main

import (
	"encoding/json"
	"net/http"
)

func confirmationHandler(w http.ResponseWriter, r *http.Request) {
	userID, err := getUserIDFromRequest(r)
	if err != nil {
		writeError(w, "unauthorized", err.Error(), nil, err)
		return
	}

	transactionID := r.URL.Query().Get("id")
	if transactionID == "" {
		writeError(w, "missing_id", "Параметр id обязателен", nil, nil)
		return
	}

	var confirmed bool
	err = db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM processed_transactions
			WHERE transaction_id = $1 AND user_id = $2
		)
	`, transactionID, userID).Scan(&confirmed)
	if err != nil {
		writeError(w, "db_error", "Ошибка базы данных", nil, err)
		return
	}

	response := struct {
		Confirmed bool `json:"confirmed"`
	}{
		Confirmed: confirmed,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
