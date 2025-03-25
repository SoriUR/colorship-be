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

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

var db *sql.DB

func main() {
	var err error

	_ = godotenv.Load()

	// Берём URL подключения из переменных окружения
	dsn := os.Getenv("SUPABASE_DB_URL")
	if dsn == "" {
		log.Fatal("SUPABASE_DB_URL is not set")
	}

	// Подключаемся к PostgreSQL (Supabase)
	db, err = sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("Ошибка подключения к БД: %v", err)
	}

	// Проверим соединение
	if err := db.Ping(); err != nil {
		log.Fatalf("Нет связи с БД: %v", err)
	}
	log.Println("Подключение к Supabase установлено!")

	http.HandleFunc("/chat", chatHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Сервер запущен на порту %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

// chatHandler – обработчик для продолжения/создания чата
func chatHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный формат JSON", http.StatusBadRequest)
		return
	}

	// 1. Получаем / создаём пользователя
	userID, err := getOrCreateUser(req.DeviceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 2. Получаем / создаём чат
	chatID := req.ChatID
	if chatID == "" {
		// Создаём новый чат (можно передать title, если надо)
		chatID, err = createChat(userID, "Мой новый чат")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

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
		// Тут можно проверить, действительно ли chatID принадлежит userID
		// или просто полагаться на клиента
	}

	// 3. Сохраняем новое сообщение пользователя
	if err := saveMessage(chatID, "user", req.Prompt); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 4. Составляем историю для OpenAI
	messages, err := getChatMessages(chatID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	openaiReq := OpenAIRequest{
		Model:    "gpt-3.5-turbo", // или "gpt-4", если есть доступ
		Messages: messages,
	}

	jsonData, err := json.Marshal(openaiReq)
	if err != nil {
		http.Error(w, "Ошибка формирования JSON для OpenAI", http.StatusInternalServerError)
		return
	}

	// 5. Отправляем запрос к OpenAI
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
		// Для отладки можно вывести сырые данные
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
		return
	}

	if len(openaiResp.Choices) == 0 {
		http.Error(w, "OpenAI не вернул ответа", http.StatusInternalServerError)
		return
	}

	assistantMsg := openaiResp.Choices[0].Message.Content

	// 6. Сохраняем ответ ассистента
	if err := saveMessage(chatID, "assistant", assistantMsg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 7. Возвращаем ответ клиенту (вместе с chatID, чтобы клиент мог продолжить чат)
	respData := ChatResponse{
		ChatID:   chatID,
		Response: assistantMsg,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(respData)
}

// getOrCreateUser получает пользователя по deviceID, при отсутствии — создаёт.
func getOrCreateUser(deviceID string) (string, error) {
	// Сначала пытаемся найти
	var userID string
	err := db.QueryRow(`
        select id from users where device_id = $1
    `, deviceID).Scan(&userID)

	if err == sql.ErrNoRows {
		// Не нашли — создаём
		err = db.QueryRow(`
            insert into users (device_id) values ($1)
            returning id
        `, deviceID).Scan(&userID)
		if err != nil {
			return "", fmt.Errorf("ошибка создания пользователя: %v", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("ошибка поиска пользователя: %v", err)
	}

	return userID, nil
}

// createChat создаёт новый чат для пользователя (при желании может принимать title)
func createChat(userID string, title string) (string, error) {
	var chatID string
	err := db.QueryRow(`
        insert into chats (user_id, title)
        values ($1, $2)
        returning id
    `, userID, title).Scan(&chatID)
	if err != nil {
		return "", fmt.Errorf("ошибка создания чата: %v", err)
	}
	return chatID, nil
}

// getChatMessages возвращает все сообщения чата
func getChatMessages(chatID string) ([]Message, error) {
	rows, err := db.Query(`
        select role, content
        from messages
        where chat_id = $1
        order by created_at asc
    `, chatID)
	if err != nil {
		return nil, fmt.Errorf("ошибка запроса сообщений чата: %v", err)
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.Role, &m.Content); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}

func saveMessage(chatID, role, content string) error {
	_, err := db.Exec(`
        insert into messages (chat_id, role, content)
        values ($1, $2, $3)
    `, chatID, role, content)
	if err != nil {
		return fmt.Errorf("ошибка сохранения сообщения: %v", err)
	}
	return nil
}
