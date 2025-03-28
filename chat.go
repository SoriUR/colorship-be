package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
)

func chatHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// Обработка GET-запроса для получения истории сообщений
		chatID := r.URL.Query().Get("chat_id")
		if chatID == "" {
			http.Error(w, "Параметр chat_id обязателен", http.StatusBadRequest)
			return
		}
		msgs, err := getChatMessages(chatID)
		if err != nil {
			http.Error(w, "Ошибка получения сообщений: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(msgs); err != nil {
			http.Error(w, "Ошибка кодирования JSON: "+err.Error(), http.StatusInternalServerError)
		}
		return

	case http.MethodPost:
		// Обработка POST-запроса для отправки нового сообщения
		var req ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Неверный формат JSON", http.StatusBadRequest)
			return
		}
		log.Println("Получен POST-запрос:", req)

		// Проверяем, что user_id передан
		userID := req.UserID
		if userID == "" {
			http.Error(w, "user_id обязателен", http.StatusBadRequest)
			return
		}
		log.Println("Проверка лимита сообщений для user_id:", userID)

		// Проверяем, есть ли уже лимит сообщений на текущий месяц
		var available bool
		log.Println("Проверяем наличие активного лимита сообщений...")
		err := db.QueryRow(`
			SELECT messages_sent < messages_limit
			FROM messages_limit
			WHERE user_id = $1 AND period_start <= now() AND period_end > now()
			LIMIT 1
		`, userID).Scan(&available)

		if err == sql.ErrNoRows {
			// Если лимита нет — проверяем, есть ли бесплатная подписка
			log.Println("Нет активного лимита, проверяем бесплатную подписку...")
			var subID string
			err = db.QueryRow(`
				SELECT id
				FROM subscriptions
				WHERE user_id = $1 AND product_name = 'free'
					AND period_start <= now() AND period_end > now()
				LIMIT 1
			`, userID).Scan(&subID)

			if err == nil {
				// Создаём лимит на этот месяц для бесплатного плана
				log.Println("Создаём новый лимит сообщений для бесплатного тарифа")
				_, err = db.Exec(`
					INSERT INTO messages_limit (
						id, user_id, subscription_id, period_start, period_end,
						product_name, messages_limit, messages_sent
					) VALUES (
						gen_random_uuid(), $1, $2,
						date_trunc('month', now()), date_trunc('month', now()) + interval '1 month',
						'free', 5, 0
					)
				`, userID, subID)

				if err != nil {
					log.Println("Ошибка при создании лимита сообщений:", err)
					http.Error(w, "Не удалось создать лимит сообщений: "+err.Error(), http.StatusInternalServerError)
					return
				}

				available = true
				log.Println("Лимит не превышен, сообщений осталось:", available)
			} else {
				log.Println("У пользователя нет активной бесплатной подписки")
				http.Error(w, "Нет активной бесплатной подписки", http.StatusForbidden)
				return
			}
		} else if err != nil {
			log.Println("Ошибка при проверке лимита:", err)
			http.Error(w, "Ошибка проверки лимита сообщений: "+err.Error(), http.StatusInternalServerError)
			return
		}

		if !available {
			log.Println("Достигнут лимит сообщений")
			http.Error(w, "Достигнут лимит сообщений в этом периоде", http.StatusForbidden)
			return
		}

		// Если chat_id пустой, создаём новый чат
		chatID := req.ChatID
		if chatID == "" {
			newChatID, err := createChat(userID, "Мой новый чат")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			chatID = newChatID

			// Добавляем system-сообщение сразу после создания чата
			systemContent := `
Ты эксперт, оценивающий идеи по шкале от 1 до 10 по уникальности, реализуемости и пользе. 
Пожалуйста, отвечай в формате Markdown.
`
			if err := saveMessage(chatID, "system", systemContent); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			// (Опционально) проверяем, что чат принадлежит user_id
			var ownerID string
			err := db.QueryRow(`SELECT user_id FROM chats WHERE id = $1`, chatID).Scan(&ownerID)
			if err == sql.ErrNoRows {
				http.Error(w, "Чат не найден", http.StatusNotFound)
				return
			} else if err != nil {
				http.Error(w, "Ошибка проверки чата: "+err.Error(), http.StatusInternalServerError)
				return
			}
			if ownerID != userID {
				http.Error(w, "Этот чат не принадлежит user_id", http.StatusForbidden)
				return
			}
		}

		// Сохраняем сообщение пользователя
		log.Println("Сохраняем сообщение пользователя")
		if err := saveMessage(chatID, "user", req.Prompt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Получаем историю чата
		messages, err := getChatMessages(chatID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Отправляем историю в OpenAI
		log.Println("Отправляем запрос в OpenAI")
		openaiReq := OpenAIRequest{
			Model:    "gpt-3.5-turbo", // или "gpt-4", если есть доступ
			Messages: messages,
		}
		jsonData, err := json.Marshal(openaiReq)
		if err != nil {
			http.Error(w, "Ошибка формирования JSON для OpenAI", http.StatusInternalServerError)
			return
		}

		openaiAPIKey := os.Getenv("OPENAI_API_KEY")
		if openaiAPIKey == "" {
			http.Error(w, "Сервер не настроен (отсутствует API ключ)", http.StatusInternalServerError)
			return
		}

		apiReq, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(jsonData))
		if err != nil {
			http.Error(w, "Ошибка создания запроса к OpenAI", http.StatusInternalServerError)
			return
		}
		apiReq.Header.Set("Content-Type", "application/json")
		apiReq.Header.Set("Authorization", "Bearer "+openaiAPIKey)

		client := &http.Client{}
		resp, err := client.Do(apiReq)
		if err != nil {
			http.Error(w, "Ошибка выполнения запроса к OpenAI", http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			http.Error(w, "Ошибка чтения ответа от OpenAI", http.StatusInternalServerError)
			return
		}

		var openaiResp OpenAIResponse
		if err := json.Unmarshal(body, &openaiResp); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Write(body)
			return
		}

		if len(openaiResp.Choices) == 0 {
			http.Error(w, "OpenAI не вернул ответа", http.StatusInternalServerError)
			return
		}

		assistantMsg := openaiResp.Choices[0].Message.Content
		log.Println("Ответ от OpenAI получен")

		// Сохраняем ответ ассистента
		if err := saveMessage(chatID, "assistant", assistantMsg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Увеличиваем счётчик отправленных сообщений
		log.Println("Увеличиваем счётчик отправленных сообщений")
		var currentSent int
		err = db.QueryRow(`
			SELECT messages_sent
			FROM messages_limit
			WHERE user_id = $1 AND period_start <= now() AND period_end > now()
			LIMIT 1
		`, userID).Scan(&currentSent)
		if err != nil {
			log.Println("Ошибка при получении текущего счётчика сообщений:", err)
		} else {
			log.Println("Текущее значение messages_sent до увеличения:", currentSent)
		}

		_, err = db.Exec(`
			UPDATE messages_limit
			SET messages_sent = messages_sent + 1
			WHERE user_id = $1 AND period_start <= now() AND period_end > now()
		`, userID)
		if err != nil {
			log.Println("Ошибка при обновлении счётчика сообщений:", err)
			http.Error(w, "Ошибка обновления счётчика сообщений: "+err.Error(), http.StatusInternalServerError)
			return
		}

		err = db.QueryRow(`
			SELECT messages_sent
			FROM messages_limit
			WHERE user_id = $1 AND period_start <= now() AND period_end > now()
			LIMIT 1
		`, userID).Scan(&currentSent)
		if err != nil {
			log.Println("Ошибка при получении messages_sent после обновления:", err)
		} else {
			log.Println("messages_sent после увеличения:", currentSent)
		}

		// Возвращаем ответ клиенту (вместе с chat_id)
		respData := ChatResponse{
			ChatID:   chatID,
			Response: assistantMsg,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(respData)
		log.Println("Ответ пользователю отправлен:", assistantMsg)
		return

	default:
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}
}

// saveMessage сохраняет сообщение в таблице messages.
func saveMessage(chatID, role, content string) error {
	_, err := db.Exec(`
        INSERT INTO messages (chat_id, role, content)
        VALUES ($1, $2, $3)
    `, chatID, role, content)
	if err != nil {
		return fmt.Errorf("ошибка сохранения сообщения: %v", err)
	}
	return nil
}

// createChat создаёт новый чат для пользователя.
func createChat(userID, title string) (string, error) {
	var chatID string
	err := db.QueryRow(`
        INSERT INTO chats (user_id, title)
        VALUES ($1, $2)
        RETURNING id
    `, userID, title).Scan(&chatID)
	if err != nil {
		return "", fmt.Errorf("ошибка создания чата: %v", err)
	}
	return chatID, nil
}

// getChatMessages возвращает все сообщения из чата, отсортированные по времени (по возрастанию).
func getChatMessages(chatID string) ([]Message, error) {
	rows, err := db.Query(`
        SELECT role, content
        FROM messages
        WHERE chat_id = $1
        ORDER BY created_at ASC
    `, chatID)
	if err != nil {
		return nil, fmt.Errorf("ошибка запроса сообщений: %v", err)
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.Role, &m.Content); err != nil {
			return nil, fmt.Errorf("ошибка сканирования сообщения: %v", err)
		}
		msgs = append(msgs, m)
	}
	// Проверка на ошибку после обхода rows
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка обхода строк: %v", err)
	}
	return msgs, nil
}
