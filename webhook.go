package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
)

func revenueCatWebhookHandler(w http.ResponseWriter, r *http.Request) {
	secretToken := "Bearer " + os.Getenv("REVENUE_CAT_WEBHOOK_TOKEN")

	authHeader := r.Header.Get("Authorization")
	if authHeader != secretToken {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		log.Println("Unauthorized webhook attempt")
		return
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		log.Println("Error reading body:", err)
		return
	}

	// Сразу отвечаем RevenueCat, чтобы не получить таймаут
	w.WriteHeader(http.StatusOK)

	// Асинхронная обработка webhook
	go handleRevenueCatEvent(body)
}

func handleRevenueCatEvent(data []byte) {
	var payload struct {
		Event struct {
			AppUserID string `json:"app_user_id"`
			Type      string `json:"type"`
		} `json:"event"`
	}

	if err := json.Unmarshal(data, &payload); err != nil {
		log.Println("Invalid JSON:", err)
		return
	}

	appUserID := payload.Event.AppUserID
	log.Printf("RevenueCat: sync user %s", appUserID)

	nonSubs, err := fetchRevenueCatCustomer(appUserID)
	if err != nil {
		log.Println("Ошибка при получении подписчика:", err)
		return
	}

	for productID, purchases := range nonSubs {
		for _, p := range purchases {
			if isTransactionProcessed(p.TransactionID) {
				continue
			}

			var count int
			switch productID {
			case "com.40apps.redflagged.messages.10":
				count = 10
			case "com.40apps.redflagged.messages.20":
				count = 20
			case "com.40apps.redflagged.messages.100":
				count = 100
			case "com.40apps.redflagged.messages.1001":
				count = 1000
			default:
				log.Printf("Неизвестный product_id: %s", productID)
				continue
			}

			if err := addPaidMessages(appUserID, count); err != nil {
				log.Printf("Не удалось начислить сообщения для %s", appUserID)
				continue
			}

			log.Printf("Начислено %d сообщений для %s", count, appUserID)
			markTransactionProcessed(appUserID, p.TransactionID, productID)
		}
	}
}

func fetchRevenueCatCustomer(appUserID string) (map[string][]NonSubPurchase, error) {
	url := fmt.Sprintf("https://api.revenuecat.com/v1/subscribers/%s", appUserID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+os.Getenv("REVENUE_CAT_API_KEY"))
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result struct {
		Subscriber struct {
			NonSubscriptions map[string][]NonSubPurchase `json:"non_subscriptions"`
		} `json:"subscriber"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return result.Subscriber.NonSubscriptions, nil
}

type NonSubPurchase struct {
	PurchaseDate  string `json:"purchase_date"`
	TransactionID string `json:"id"`
}

func isTransactionProcessed(txID string) bool {
	var exists bool
	err := db.QueryRow(`select exists(select 1 from processed_transactions where transaction_id = $1)`, txID).Scan(&exists)
	if err != nil {
		log.Println("Ошибка при проверке транзакции:", err)
		return true // fail-safe: лучше не начислять
	}
	return exists
}

func markTransactionProcessed(userID string, txID string, productID string) {
	_, err := db.Exec(`insert into processed_transactions (id, user_id, transaction_id, product_id, created_at)
		values (gen_random_uuid(), $1, $2, $3, now())`, userID, txID, productID)
	if err != nil {
		log.Println("Ошибка при сохранении транзакции:", err)
	}
}

func addPaidMessages(userID string, count int) error {
	_, err := db.Exec(`
		update user_credits
		set
			count = count + $1,
			is_using_paid = true,
			updated_at = now()
		where user_id = $2
	`, count, userID)
	if err != nil {
		log.Printf("Ошибка при начислении сообщений: %v", err)
	}
	return err
}
