package main

type ChatRequest struct {
	UserID string `json:"user_id"` // теперь клиент передаёт user_id
	ChatID string `json:"chat_id"` // если пустой, создаётся новый чат
	Prompt string `json:"prompt"`
}

type ChatResponse struct {
	ChatID   string `json:"chat_id"`
	Response string `json:"response"`
}

type Message struct {
	Role    string `json:"role"`    // "system", "user", "assistant"
	Content string `json:"content"` // текст сообщения
}

type OpenAIRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type Choice struct {
	Message Message `json:"message"`
}

type OpenAIResponse struct {
	Choices []Choice `json:"choices"`
}

type ChatSummary struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
}
