package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	http.HandleFunc("/chat", chatHandler)
	// Получаем порт из переменной окружения Railway, по умолчанию 8080
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Сервер запущен на порту %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func chatHandler(w http.ResponseWriter, r *http.Request) {
	// Разрешаем только POST-запросы
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	// Декодируем JSON-запрос от iOS
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный формат запроса", http.StatusBadRequest)
		return
	}

	// Получаем API-ключ из переменных окружения
	openaiAPIKey := os.Getenv("OPENAI_API_KEY")
	if openaiAPIKey == "" {
		http.Error(w, "Сервер не настроен (отсутствует API ключ)", http.StatusInternalServerError)
		return
	}

	// Формируем запрос к OpenAI
	openaiReq := OpenAIRequest{
		Model: "gpt-3.5-turbo", // Можно заменить на "gpt-4", если доступен
		Messages: []Message{
			{
				Role:    "system",
				Content: "Ты эксперт, оценивающий идеи по шкале от 1 до 10 по уникальности, реализуемости и пользе. Дай краткий комментарий в формате Markdown.",
			},
			{
				Role:    "user",
				Content: req.Prompt,
			},
		},
	}
	jsonData, err := json.Marshal(openaiReq)
	if err != nil {
		http.Error(w, "Ошибка формирования запроса", http.StatusInternalServerError)
		return
	}

	// Создаём запрос к OpenAI API
	openaiAPIURL := "https://api.openai.com/v1/chat/completions"
	apiReq, err := http.NewRequest("POST", openaiAPIURL, bytes.NewBuffer(jsonData))
	if err != nil {
		http.Error(w, "Ошибка формирования запроса", http.StatusInternalServerError)
		return
	}
	apiReq.Header.Set("Content-Type", "application/json")
	apiReq.Header.Set("Authorization", "Bearer "+openaiAPIKey)

	// Отправляем запрос
	client := &http.Client{}
	resp, err := client.Do(apiReq)
	if err != nil {
		http.Error(w, "Ошибка вызова OpenAI API", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Ошибка чтения ответа OpenAI", http.StatusInternalServerError)
		return
	}

	// Парсим ответ
	var openaiResp OpenAIResponse
	if err := json.Unmarshal(body, &openaiResp); err != nil {
		// Если не получилось распарсить, возвращаем сырые данные
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
		return
	}

	// Если есть хотя бы один вариант, выбираем первый
	if len(openaiResp.Choices) > 0 {
		answer := openaiResp.Choices[0].Message.Content
		response := map[string]string{"response": answer}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	} else {
		http.Error(w, "Нет ответа от OpenAI", http.StatusInternalServerError)
	}
}
