package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
)

func chatsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	// Берём user_id из URL, а не device_id
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		http.Error(w, "Параметр user_id обязателен", http.StatusBadRequest)
		return
	}

	// Запрашиваем чаты, где поле user_id соответствует переданному значению
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

// getOrCreateUser получает пользователя по deviceID, при отсутствии — создаёт.
func getOrCreateUser(deviceID string) (string, error) {
	var userID string
	// Пытаемся найти пользователя по deviceID
	err := db.QueryRow(`
        SELECT id FROM users WHERE device_id = $1
    `, deviceID).Scan(&userID)
	if err == sql.ErrNoRows {
		// Пользователь не найден – создаём нового
		err = db.QueryRow(`
            INSERT INTO users (device_id) VALUES ($1)
            RETURNING id
        `, deviceID).Scan(&userID)
		if err != nil {
			return "", fmt.Errorf("ошибка создания пользователя: %v", err)
		}
	} else if err != nil {
		// Какая-то другая ошибка при поиске пользователя
		return "", fmt.Errorf("ошибка поиска пользователя: %v", err)
	}
	return userID, nil
}
