package main

// ChatRequest – структура запроса от iOS, содержащая промт
type ChatRequest struct {
	DeviceID string `json:"device_id"`
	ChatID   string `json:"chat_id"`
	Prompt   string `json:"prompt"`
}

// ChatResponse – тело ответа клиенту
type ChatResponse struct {
	ChatID   string `json:"chat_id"`
	Response string `json:"response"`
}

// Message – структура сообщения для OpenAI
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// OpenAIRequest – структура запроса к OpenAI API
type OpenAIRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

// Choice – структура для выбора ответа от OpenAI
type Choice struct {
	Message Message `json:"message"`
}

// OpenAIResponse – структура ответа от OpenAI API
type OpenAIResponse struct {
	Choices []Choice `json:"choices"`
}
