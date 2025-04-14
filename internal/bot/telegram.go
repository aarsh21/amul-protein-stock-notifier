package bot

import (
	"amul-notifier/internal/config"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

func StartupTestNotification(appConfig *config.AppConfig) error {
	testMessage := fmt.Sprintf("Amul Stock Notifier started successfully! Monitoring %d SKUs. Quiet hours: %d:00-%d:00 %s.", len(appConfig.MonitoredSKUsMap), quietHourStart, quietHourEnd, appConfig.Timezone.String())
	err := sendTelegramNotification(testMessage, appConfig)
	if err != nil {
		if !isQuietHours(appConfig.Timezone) {
			return err
			// log.Fatalf("Failed to send test notification (outside quiet hours): %v. Check Telegram config.", err)
		} else {
			log.Printf("Test notification suppressed due to quiet hours.")
		}
	} else {
		log.Println("Test notification sent successfully (or suppressed due to quiet hours).")
	}
	return nil
}

func isQuietHours(loc *time.Location) bool {
	if loc == nil {
		log.Printf("Warning: Time location is nil, cannot check quiet hours. Assuming it's NOT quiet hours.")
		return false
	}
	currentTime := time.Now().In(loc)
	currentHour := currentTime.Hour()
	return currentHour >= quietHourStart && currentHour < quietHourEnd
}

func sendTelegramNotification(message string, appConfig *config.AppConfig) error {
	if isQuietHours(appConfig.Timezone) {
		log.Printf("Telegram notification suppressed due to quiet hours (%d:00-%d:00 %s).", quietHourStart, quietHourEnd, appConfig.Timezone.String())
		return nil
	}

	if appConfig.TelegramBotToken == "" || appConfig.TelegramChatId == "" {
		log.Println("Error: Attempted to send Telegram notification but token or chat ID is missing.")
		return fmt.Errorf("telegram bot token or chat id is not configured")
	}

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", appConfig.TelegramBotToken)

	payload := map[string]string{
		"chat_id":                  appConfig.TelegramChatId,
		"text":                     message,
		"parse_mode":               "HTML",
		"disable_web_page_preview": "false",
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshalling telegram payload: %v", err)
		return fmt.Errorf("error marshalling telegram payload: %w", err)
	}
	log.Printf("Attempting to send Telegram payload to chat ID %s...", appConfig.TelegramChatId)

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		log.Printf("Error creating Telegram request: %v", err)
		return fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "AmulStockNotifier/1.2")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error sending request to Telegram API: %v", err)
		return fmt.Errorf("error sending request to telegram api: %w", err)
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		log.Printf("Error reading Telegram response body (Status: %s): %v", resp.Status, readErr)
		return fmt.Errorf("error reading telegram response body (status %d): %w", resp.StatusCode, readErr)
	}

	log.Printf("Telegram API response Status: %s", resp.Status)
	if resp.StatusCode != http.StatusOK {
		log.Printf("Telegram API response Body (Error): %s", string(body))
		log.Printf("Error: Telegram API returned non-OK status: %d", resp.StatusCode)
		return fmt.Errorf("telegram api returned status %d: %s", resp.StatusCode, string(body))
	}

	var telegramResponse map[string]any
	if err := json.Unmarshal(body, &telegramResponse); err != nil {
		log.Printf("Warning: Could not parse successful Telegram response JSON: %v", err)
	} else {
		if ok, exists := telegramResponse["ok"].(bool); !exists || !ok {
			log.Printf("Error: Telegram API status OK, but response indicates failure: %s", string(body))
			return fmt.Errorf("telegram api reported failure despite 200 OK: %s", string(body))
		}
	}

	log.Printf("Telegram request successful (Status: %s)", resp.Status)
	return nil
}

func SendInitialStockNotifications(bot *Bot) {
	log.Println("Checking for products already in stock at startup...")

	inStockMessages := []string{}

	for sku := range bot.appConfig.MonitoredSKUsMap {
		if inStock, exists := bot.productStockState[sku]; exists && inStock {
			prodInfo, detailsExist := bot.productDetails[sku]
			name := "Unknown Product"
			alias := ""
			inventory := 0
			if detailsExist {
				name = prodInfo.Name
				alias = prodInfo.Alias
				inventory = prodInfo.InventoryQuantity
			} else {
				log.Printf("Warning: Details missing for initially in-stock SKU %s", sku)
			}

			log.Printf("Found monitored product already in stock at startup: %s (SKU: %s)", name, sku)

			link := ""
			if alias != "" {
				link = fmt.Sprintf("\n🔗 <a href=\"%s%s\">View on Amul Shop</a>", productBaseURL, alias)
			}

			message := fmt.Sprintf("• <b>%s</b> (SKU: %s) - Qty: %d %s", name, sku, inventory, link)
			inStockMessages = append(inStockMessages, message)
		}
	}

	if len(inStockMessages) > 0 {
		fullMessage := "<b>Initial Stock Alert!</b>\n\nThese monitored products are currently IN STOCK:\n" +
			strings.Join(inStockMessages, "\n")

		err := sendTelegramNotification(fullMessage, bot.appConfig)
		if err != nil {
			if !isQuietHours(bot.appConfig.Timezone) {
				log.Printf("Error sending initial stock notification: %v", err)
			}
		} else {
			log.Println("Initial stock notification sent (or suppressed).")
		}
	} else {
		log.Println("No monitored products found in stock at startup.")
	}
}

func sendNotificationWithRetry(appConfig *config.AppConfig, message, sku, notificationType string) {
	if isQuietHours(appConfig.Timezone) {
		log.Printf("Notification (%s) for SKU %s suppressed due to quiet hours.", notificationType, sku)
		return
	}

	var notifErr error
	for attempts := range 3 {
		notifErr = sendTelegramNotification(message, appConfig)
		if notifErr == nil {
			log.Printf("Telegram notification (%s) sent successfully for %s (Attempt %d).", notificationType, sku, attempts+1)
			return
		}

		log.Printf("Attempt %d: Error sending Telegram notification (%s) for %s: %v",
			attempts+1, notificationType, sku, notifErr)

		if attempts < 2 {
			time.Sleep(2 * time.Second)
		}

	}
	log.Printf("FAILED to send Telegram notification (%s) after 3 attempts for %s", notificationType, sku)
}
