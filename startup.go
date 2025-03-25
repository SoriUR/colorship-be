package main

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// startupHandler принимает device_id, получает или создаёт пользователя и возвращает user_id.
func startupHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	// Структура запроса с device_id
	var req struct {
		DeviceID string `json:"device_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DeviceID == "" {
		http.Error(w, "Неверный формат запроса или отсутствует device_id", http.StatusBadRequest)
		return
	}

	// Получаем или создаём пользователя по device_id
	userID, err := getOrCreateUser(req.DeviceID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Ошибка получения/создания пользователя: %v", err), http.StatusInternalServerError)
		return
	}

	// Возвращаем user_id в формате JSON
	respData := struct {
		UserID string `json:"user_id"`
	}{
		UserID: userID,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(respData)
}
