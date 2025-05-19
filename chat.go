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
	"strings"
)

func chatHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("chatHandler: method=%s", r.Method)

	switch r.Method {
	case http.MethodGet:
		handleChatGet(w, r)
	case http.MethodPost:
		handleChatPost(w, r)
	default:
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
	}
}

func handleChatGet(w http.ResponseWriter, r *http.Request) {
	chatID := r.URL.Query().Get("chat_id")
	if chatID == "" {
		log.Println("handleChatGet error: Параметр chat_id обязателен")
		http.Error(w, "Параметр chat_id обязателен", http.StatusBadRequest)
		return
	}
	msgs, err := getChatMessages(chatID)
	if err != nil {
		log.Println("handleChatGet error: Ошибка получения сообщений")
		http.Error(w, "Ошибка получения сообщений: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(msgs); err != nil {
		http.Error(w, "Ошибка кодирования JSON: "+err.Error(), http.StatusInternalServerError)
	}
}

func handleChatPost(w http.ResponseWriter, r *http.Request) {
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный формат JSON", http.StatusBadRequest)
		return
	}
	log.Println("Получен POST-запрос:", req)

	userID, err := getUserIDFromRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	var freeLeft, paidLeft int
	err = db.QueryRow(`
		SELECT free_messages_left, paid_messages_left
		FROM user_credits
		WHERE user_id = $1
	`, userID).Scan(&freeLeft, &paidLeft)
	if err != nil {
		log.Println("handleChatPost error: Ошибка получения лимита сообщений")
		http.Error(w, "Ошибка получения лимита сообщений", http.StatusInternalServerError)
		return
	}
	if freeLeft <= 0 && paidLeft <= 0 {
		writeError(w, "no_messages", "У вас закончились все доступные сообщения", nil)
		return
	}

	if len(req.ImagePaths) > 0 && paidLeft == 0 {
		writeError(w, "images_not_allowed_for_free", "Изображения доступны только при наличии платных сообщений", nil)
		return
	}

	chatID := req.ChatID
	if chatID == "" {
		// Новый чат
		title := req.Prompt
		if len(title) > 50 {
			title = title[:50]
		}
		newChatID, err := createChat(userID, title)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		chatID = newChatID

		systemContent := os.Getenv("GPT_PROMPT")
		if systemContent == "" {
			http.Error(w, "Не удалось прочитать system prompt: "+err.Error(), http.StatusInternalServerError)
			return
		}

		if err := saveMessage(chatID, "system", systemContent); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		var ownerID string
		err := db.QueryRow(`SELECT user_id FROM chats WHERE id = $1`, chatID).Scan(&ownerID)
		if err == sql.ErrNoRows {
			log.Println("handleChatPost error: Чат не найден")
			http.Error(w, "Чат не найден", http.StatusNotFound)
			return
		} else if err != nil {
			log.Println("handleChatPost error: Ошибка проверки чата")
			http.Error(w, "Ошибка проверки чата: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if ownerID != userID {
			log.Println("handleChatPost error: Этот чат не принадлежит user_id")
			http.Error(w, "Этот чат не принадлежит user_id", http.StatusForbidden)
			return
		}
	}

	if err := saveMessage(chatID, "user", req.Prompt); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	messages, err := getChatMessages(chatID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	openaiAPIKey := os.Getenv("OPENAI_API_KEY")
	if openaiAPIKey == "" {
		log.Println("handleChatPost error: Сервер не настроен (отсутствует API ключ)")
		http.Error(w, "Сервер не настроен (отсутствует API ключ)", http.StatusInternalServerError)
		return
	}

	var visionContents []VisionContentItem
	for _, msg := range messages {
		if strings.HasPrefix(msg.Content, "image:") {
			path := strings.TrimSpace(strings.TrimPrefix(msg.Content, "image:"))
			signedURL, err := getSignedURL(path)
			if err != nil {
				log.Println("handleChatPost error: Ошибка получения signed URL из истории")
				log.Println("Ошибка получения signed URL из истории:", err)
				continue
			}
			visionContents = append(visionContents, VisionContentItem{
				Type: "image_url",
				ImageURL: &VisionImageURL{
					URL:    signedURL,
					Detail: "auto",
				},
			})
		} else {
			visionContents = append(visionContents, VisionContentItem{
				Type: "text",
				Text: msg.Content,
			})
		}
	}

	// Добавляем текущий prompt
	visionContents = append(visionContents, VisionContentItem{
		Type: "text",
		Text: req.Prompt,
	})

	// Добавляем картинки из текущего запроса
	for _, path := range req.ImagePaths {
		signedURL, err := getSignedURL(path)
		if err != nil {
			log.Println("handleChatPost error: Ошибка получения signed URL")
			http.Error(w, "Ошибка получения signed URL: "+err.Error(), http.StatusInternalServerError)
			return
		}
		visionContents = append(visionContents, VisionContentItem{
			Type: "image_url",
			ImageURL: &VisionImageURL{
				URL:    signedURL,
				Detail: "auto",
			},
		})
	}

	model := "gpt-3.5-turbo"
	if paidLeft > 0 {
		model = "gpt-4o"
	}

	visionReq := VisionRequest{
		Model: model,
		Messages: []VisionMessage{{
			Role:    "user",
			Content: visionContents,
		}},
	}

	jsonData, err := json.Marshal(visionReq)
	if err != nil {
		log.Println("handleChatPost error: Ошибка формирования JSON для OpenAI")
		http.Error(w, "Ошибка формирования JSON для OpenAI", http.StatusInternalServerError)
		return
	}

	apiReq, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Println("handleChatPost error: Ошибка создания запроса к OpenAI")
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

	log.Printf("vision JSON: %s", string(jsonData))
	log.Printf("OpenAI raw response: %s", string(body))

	if len(openaiResp.Choices) == 0 {
		http.Error(w, "OpenAI не вернул ответа", http.StatusInternalServerError)
		return
	}

	log.Println("8")

	assistantMsg := openaiResp.Choices[0].Message.Content
	if err := saveMessage(chatID, "assistant", assistantMsg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Обновляем счётчик сообщений в user_credits
	if paidLeft > 0 {
		_, err = db.Exec(`UPDATE user_credits SET paid_messages_left = paid_messages_left - 1 WHERE user_id = $1`, userID)
	} else {
		_, err = db.Exec(`UPDATE user_credits SET free_messages_left = free_messages_left - 1 WHERE user_id = $1`, userID)
	}
	if err != nil {
		log.Println("handleChatPost error: Ошибка обновления счётчика сообщений")
		http.Error(w, "Ошибка обновления счётчика сообщений: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Println("9")

	respData := ChatResponse{
		ChatID:   chatID,
		Response: assistantMsg,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(respData)
}

func getSignedURL(path string) (string, error) {
	baseURL := os.Getenv("SUPABASE_URL") + "/storage/v1"
	secret := os.Getenv("SUPABASE_SERVICE_ROLE")
	bucket := os.Getenv("SUPABASE_BUCKET_NAME")

	if baseURL == "" || secret == "" || bucket == "" {
		return "", fmt.Errorf("не заданы переменные окружения SUPABASE_URL / SERVICE_ROLE / BUCKET_NAME")
	}

	requestBody := map[string]interface{}{
		"expiresIn": 3600, // 1 час
	}
	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/object/sign/%s/%s", baseURL, bucket, strings.TrimPrefix(path, "/"))

	fmt.Println(url)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+secret)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		return "", fmt.Errorf("supabase вернул %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		SignedURL string `json:"signedURL"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", err
	}

	return baseURL + response.SignedURL, nil
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
