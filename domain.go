package main

type ChatRequest struct {
	UserID     string   `json:"user_id"` // теперь клиент передаёт user_id
	ChatID     string   `json:"chat_id"` // если пустой, создаётся новый чат
	Prompt     string   `json:"prompt"`
	ImagePaths []string `json:"image_paths"`
	VoicePaths []string `json:"voice_paths"`
}

type ChatResponse struct {
	ChatID   string `json:"chat_id"`
	Response string `json:"response"`
}

type Message struct {
	Role              string   `json:"role"`
	Content           string   `json:"content"`
	ImagePaths        []string `json:"image_paths"`
	VoicePaths        []string `json:"voice_paths"`
	VoiceTranscription string   `json:"voice_transcription,omitempty"`
	Timestamp         string   `json:"timestamp"`
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

type VisionRequest struct {
	Model    string          `json:"model"`
	Messages []VisionMessage `json:"messages"`
}

type VisionMessage struct {
	Role    string              `json:"role"`
	Content []VisionContentItem `json:"content"`
}

type VisionContentItem struct {
	Type     string          `json:"type"`                // "text" или "image_url"
	Text     string          `json:"text,omitempty"`      // если Type == "text"
	ImageURL *VisionImageURL `json:"image_url,omitempty"` // если Type == "image_url"
}

type VisionImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail"` // "low", "high", "auto"
}
