package main

// ChatRequest – структура запроса от iOS, содержащая промт
type ChatRequest struct {
	Prompt string `json:"prompt"`
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
