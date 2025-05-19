package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
)

func chatsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	userID, err := getUserIDFromRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	rows, err := db.Query(`
		SELECT id, title
		FROM chats
		WHERE user_id = $1
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		http.Error(w, "Ошибка запроса чатов: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var chats []ChatSummary
	for rows.Next() {
		var cs ChatSummary
		var title sql.NullString
		if err := rows.Scan(&cs.ID, &title); err != nil {
			http.Error(w, "Ошибка чтения данных: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if title.Valid {
			cs.Title = title.String
		}
		chats = append(chats, cs)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(chats)
}
