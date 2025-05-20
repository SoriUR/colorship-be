package main

import (
	"database/sql"
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

	http.HandleFunc("/api/launch", launchHandler)
	http.HandleFunc("/api/sign_up", signUpHandler)
	http.HandleFunc("/api/chat", chatHandler)
	http.HandleFunc("/api/chats", chatsHandler)
	http.HandleFunc("/api/webhook/revenuecat", revenueCatWebhookHandler)
	http.HandleFunc("/api/confirmation", confirmationHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Сервер запущен на порту %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
