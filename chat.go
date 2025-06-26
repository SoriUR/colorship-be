package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"strings"

	"github.com/lib/pq"
)

func chatHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("chatHandler: method=%s", r.Method)

	switch r.Method {
	case http.MethodGet:
		handleChatGet(w, r)
	case http.MethodPost:
		handleChatPost(w, r)
	default:
		writeError(w, "method_not_allowed", "Метод не поддерживается", nil, nil)
	}
}

func handleChatGet(w http.ResponseWriter, r *http.Request) {
	chatID := r.URL.Query().Get("chat_id")
	if chatID == "" {
		log.Println("handleChatGet error: Параметр chat_id обязателен")
		writeError(w, "missing_chat_id", "Параметр chat_id обязателен", nil, nil)
		return
	}
	msgs, err := getChatMessages(chatID, false)
	if err != nil {
		log.Println("handleChatGet error: Ошибка получения сообщений")
		writeError(w, "db_error", "Ошибка получения сообщений", nil, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(msgs); err != nil {
		writeError(w, "json_encode_error", "Ошибка кодирования JSON", nil, err)
	}
}

func handleChatPost(w http.ResponseWriter, r *http.Request) {
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "json_decode_error", "Неверный формат JSON", nil, err)
		return
	}
	log.Println("Получен POST-запрос:", req)

	userID, err := getUserIDFromRequest(r)
	if err != nil {
		writeError(w, "unauthorized", err.Error(), nil, err)
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
		writeError(w, "db_error", "Ошибка получения лимита сообщений", nil, err)
		return
	}
	if freeLeft <= 0 && paidLeft <= 0 {
		writeError(w, "no_messages", "У вас закончились все доступные сообщения", nil, nil)
		return
	}

	if len(req.ImagePaths) > 0 && paidLeft == 0 {
		writeError(w, "images_not_allowed_for_free", "Изображения доступны только при наличии платных сообщений", nil, nil)
		return
	}

	if len(req.VoicePaths) > 0 && paidLeft == 0 {
		writeError(w, "images_not_allowed_for_free", "Голосовые сообщения доступны только при наличии платных сообщений", nil, nil)
		return
	}

	chatID := req.ChatID
	if chatID == "" {
		// Новый чат
		title := req.Prompt
		if title == "" {
			title = "New Chat"
		}
		if len(title) > 50 {
			title = title[:50]
		}
		newChatID, err := createChat(userID, title)
		if err != nil {
			writeError(w, "db_error", "Ошибка создания чата", nil, err)
			return
		}
		chatID = newChatID

		systemBytes, err := os.ReadFile(".prompt")
		if err != nil {
			writeError(w, "file_read_error", "Не удалось прочитать .prompt файл", nil, err)
			return
		}
		systemContent := string(systemBytes)
		log.Printf("Загруженный system prompt: %s", systemContent)

		if err := saveMessage(chatID, "system", systemContent, nil); err != nil {
			writeError(w, "db_error", "Ошибка сохранения system-сообщения", nil, err)
			return
		}
	} else {
		var ownerID string
		err := db.QueryRow(`SELECT user_id FROM chats WHERE id = $1`, chatID).Scan(&ownerID)
		if err == sql.ErrNoRows {
			log.Println("handleChatPost error: Чат не найден")
			writeError(w, "not_found", "Чат не найден", nil, nil)
			return
		} else if err != nil {
			log.Println("handleChatPost error: Ошибка проверки чата")
			writeError(w, "db_error", "Ошибка проверки чата", nil, err)
			return
		}
		if ownerID != userID {
			log.Println("handleChatPost error: Этот чат не принадлежит user_id")
			writeError(w, "forbidden", "Этот чат не принадлежит user_id", nil, nil)
			return
		}
	}

	// Транскрибируем голосовые сообщения один раз для текущего запроса
	var currentVoiceTranscription string
	if len(req.VoicePaths) > 0 {
		transcription, err := transcribeVoiceFiles(req.VoicePaths)
		if err != nil {
			log.Println("handleChatPost error: Ошибка транскрипции голоса")
			writeError(w, "voice_transcription_error", "Ошибка транскрипции голосового сообщения", nil, err)
			return
		}
		currentVoiceTranscription = transcription
	}

	if err := saveMessageWithTranscription(chatID, "user", req.Prompt, req.ImagePaths, req.VoicePaths, currentVoiceTranscription); err != nil {
		writeError(w, "db_error", "Ошибка сохранения сообщения пользователя", nil, err)
		return
	}

	messages, err := getChatMessages(chatID, true)
	if err != nil {
		writeError(w, "db_error", "Ошибка получения сообщений", nil, err)
		return
	}

	openaiAPIKey := os.Getenv("OPENAI_API_KEY")
	if openaiAPIKey == "" {
		log.Println("handleChatPost error: Сервер не настроен (отсутствует API ключ)")
		writeError(w, "openai_not_configured", "Сервер не настроен (отсутствует API ключ)", nil, nil)
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

		// Добавляем кэшированные транскрипции голосовых сообщений из истории
		if msg.VoiceTranscription != "" {
			visionContents = append(visionContents, VisionContentItem{
				Type: "text",
				Text: msg.VoiceTranscription,
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
			writeError(w, "supabase_signed_url_error", "Ошибка получения signed URL", nil, err)
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

	// Добавляем транскрипцию текущих голосовых сообщений
	if currentVoiceTranscription != "" {
		visionContents = append(visionContents, VisionContentItem{
			Type: "text",
			Text: currentVoiceTranscription,
		})
	}

	var model string = "gpt-4o"

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
		writeError(w, "json_encode_error", "Ошибка формирования JSON для OpenAI", nil, err)
		return
	}

	apiReq, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Println("handleChatPost error: Ошибка создания запроса к OpenAI")
		writeError(w, "openai_error", "Ошибка создания запроса к OpenAI", nil, err)
		return
	}
	apiReq.Header.Set("Content-Type", "application/json")
	apiReq.Header.Set("Authorization", "Bearer "+openaiAPIKey)

	client := &http.Client{}
	resp, err := client.Do(apiReq)
	if err != nil {
		writeError(w, "openai_error", "Ошибка выполнения запроса к OpenAI", nil, err)
		return
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		writeError(w, "openai_error", "Ошибка чтения ответа от OpenAI", nil, err)
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
		writeError(w, "openai_error", "OpenAI не вернул ответа", nil, nil)
		return
	}

	assistantMsg := openaiResp.Choices[0].Message.Content
	if err := saveMessage(chatID, "assistant", assistantMsg, nil); err != nil {
		writeError(w, "db_error", "Ошибка сохранения сообщения ассистента", nil, err)
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
		writeError(w, "db_error", "Ошибка обновления счётчика сообщений", nil, err)
		return
	}
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

func getVoiceSignedURL(path string) (string, error) {
	baseURL := os.Getenv("SUPABASE_URL") + "/storage/v1"
	secret := os.Getenv("SUPABASE_SERVICE_ROLE")
	voiceBucket := "redflagged-voices" // Hardcoded voice bucket name

	if baseURL == "" || secret == "" {
		return "", fmt.Errorf("не заданы переменные окружения SUPABASE_URL / SERVICE_ROLE")
	}

	requestBody := map[string]interface{}{
		"expiresIn": 3600, // 1 час
	}
	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/object/sign/%s/%s", baseURL, voiceBucket, strings.TrimPrefix(path, "/"))

	fmt.Printf("Voice signed URL: %s\n", url)

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
		body, _ := io.ReadAll(resp.Body)
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

// saveMessage сохраняет сообщение в таблице messages (для системных сообщений без голоса).
func saveMessage(chatID, role, content string, imagePaths []string, voicePaths ...[]string) error {
	var voices []string
	if len(voicePaths) > 0 {
		voices = voicePaths[0]
	}
	return saveMessageWithTranscription(chatID, role, content, imagePaths, voices, "")
}

// saveMessageWithTranscription сохраняет сообщение с уже готовой транскрипцией.
func saveMessageWithTranscription(chatID, role, content string, imagePaths, voicePaths []string, voiceTranscription string) error {
	_, err := db.Exec(`
        INSERT INTO messages (chat_id, role, content, image_paths, voice_paths, voice_transcription)
        VALUES ($1, $2, $3, $4, $5, $6)
    `, chatID, role, content, pq.Array(imagePaths), pq.Array(voicePaths), voiceTranscription)
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

// getChatMessages возвращает все сообщения из чата, отсортированные по времени (по возрастанию). Если includeSystem == false, исключает system-сообщения.
func getChatMessages(chatID string, includeSystem bool) ([]Message, error) {
	query := `
        SELECT role, content, image_paths, voice_paths, voice_transcription, created_at
        FROM messages
        WHERE chat_id = $1`
	if !includeSystem {
		query += " AND role != 'system'"
	}
	query += " ORDER BY created_at ASC"

	rows, err := db.Query(query, chatID)
	if err != nil {
		return nil, fmt.Errorf("ошибка запроса сообщений: %v", err)
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		var voiceTranscription sql.NullString
		var timestamp sql.NullTime
		err := rows.Scan(&m.Role, &m.Content, pq.Array(&m.ImagePaths), pq.Array(&m.VoicePaths), &voiceTranscription, &timestamp)
		if err != nil {
			return nil, fmt.Errorf("ошибка сканирования сообщения: %v", err)
		}
		
		// Only include voice transcription for internal processing (includeSystem=true)
		if voiceTranscription.Valid && includeSystem {
			m.VoiceTranscription = voiceTranscription.String
		}
		
		if timestamp.Valid {
			m.Timestamp = timestamp.Time.Format("2006-01-02T15:04:05Z")
		}
		
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка обхода строк: %v", err)
	}
	return msgs, nil
}

// transcribeVoiceFiles transcribes multiple voice files and returns their combined text
func transcribeVoiceFiles(voicePaths []string) (string, error) {
	if len(voicePaths) == 0 {
		return "", nil
	}

	var transcriptions []string
	for _, path := range voicePaths {
		signedURL, err := getVoiceSignedURL(path)
		if err != nil {
			log.Printf("Ошибка получения voice signed URL для голоса %s: %v", path, err)
			continue
		}

		transcription, err := transcribeVoiceFromURL(signedURL)
		if err != nil {
			log.Printf("Ошибка транскрипции голоса %s: %v", path, err)
			continue
		}

		if transcription != "" {
			transcriptions = append(transcriptions, transcription)
		}
	}

	if len(transcriptions) == 0 {
		return "", fmt.Errorf("не удалось транскрибировать ни одного голосового сообщения")
	}

	return strings.Join(transcriptions, "\n"), nil
}

// transcribeVoiceFromURL downloads and transcribes audio from URL
func transcribeVoiceFromURL(audioURL string) (string, error) {
	openaiAPIKey := os.Getenv("OPENAI_API_KEY")
	if openaiAPIKey == "" {
		return "", fmt.Errorf("сервер не настроен (отсутствует API ключ)")
	}

	// Скачиваем аудиофайл
	resp, err := http.Get(audioURL)
	if err != nil {
		return "", fmt.Errorf("ошибка скачивания аудио: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ошибка скачивания аудио, статус: %d", resp.StatusCode)
	}

	audioData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка чтения аудиоданных: %v", err)
	}

	// Создаем multipart form для отправки в OpenAI
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	// Добавляем модель
	err = writer.WriteField("model", "whisper-1")
	if err != nil {
		return "", fmt.Errorf("ошибка создания поля model: %v", err)
	}

	// Добавляем аудиофайл
	fileWriter, err := writer.CreateFormFile("file", "audio.m4a")
	if err != nil {
		return "", fmt.Errorf("ошибка создания поля file: %v", err)
	}

	_, err = fileWriter.Write(audioData)
	if err != nil {
		return "", fmt.Errorf("ошибка записи аудиоданных: %v", err)
	}

	err = writer.Close()
	if err != nil {
		return "", fmt.Errorf("ошибка закрытия writer: %v", err)
	}

	// Создаем HTTP запрос
	req, err := http.NewRequest("POST", "https://api.openai.com/v1/audio/transcriptions", &requestBody)
	if err != nil {
		return "", fmt.Errorf("ошибка создания запроса: %v", err)
	}

	req.Header.Set("Authorization", "Bearer "+openaiAPIKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Выполняем запрос
	client := &http.Client{}
	resp2, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ошибка выполнения запроса к OpenAI: %v", err)
	}
	defer resp2.Body.Close()

	// Читаем ответ
	body, err := io.ReadAll(resp2.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка чтения ответа: %v", err)
	}

	if resp2.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OpenAI вернул ошибку %d: %s", resp2.StatusCode, string(body))
	}

	// Парсим JSON ответ
	var transcriptionResponse struct {
		Text string `json:"text"`
	}

	err = json.Unmarshal(body, &transcriptionResponse)
	if err != nil {
		return "", fmt.Errorf("ошибка парсинга JSON ответа: %v", err)
	}

	log.Printf("Транскрипция успешно выполнена: %s", transcriptionResponse.Text)
	return transcriptionResponse.Text, nil
}
