package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// TODO: split the code into different files

// Struct to match the overall JSON response structure
type ProductListResponse struct {
	Data []ProductInfo `json:"data"`
}

// Struct for individual product information within the Data array
type ProductInfo struct {
	ID                string `json:"_id"`
	Name              string `json:"name"`
	Alias             string `json:"alias"`
	SKU               string `json:"sku"`
	Available         int    `json:"available"` // 1 if available, likely 0 otherwise
	InventoryQuantity int    `json:"inventory_quantity"`
	Price             int    `json:"price"`
}

const (
	// API URL fetching products in the 'protein' category (adjust filters if needed)
	apiURL = "https://shop.amul.com/api/1/entity/ms.products?fields[name]=1&fields[brand]=1&fields[categories]=1&fields[collections]=1&fields[alias]=1&fields[sku]=1&fields[price]=1&fields[compare_price]=1&fields[original_price]=1&fields[images]=1&fields[metafields]=1&fields[discounts]=1&fields[catalog_only]=1&fields[is_catalog]=1&fields[seller]=1&fields[available]=1&fields[inventory_quantity]=1&fields[net_quantity]=1&fields[num_reviews]=1&fields[avg_rating]=1&fields[inventory_low_stock_quantity]=1&fields[inventory_allow_out_of_stock]=1&filters[0][field]=categories&filters[0][value][0]=protein&filters[0][operator]=in&facets=true&facetgroup=default_category_facet&limit=100&total=1&start=0"

	productBaseURL = "https://shop.amul.com/en/product/"

	// --- Quiet Hours Configuration (IST) ---
	quietHourStart = 0 // 12:00 AM
	quietHourEnd   = 7 // Up to 6:59:59 AM (exclusive of 7)
	timeZone       = "Asia/Kolkata"

	// TODO: parse the expiry time and generate one more cookie again
	cookieRefreshMargin = 90 * time.Hour // Refresh cookie before it expires
)

// --- Global state ---
var (
	productStockState = make(map[string]bool)        // SKU -> inStock (bool)
	productDetails    = make(map[string]ProductInfo) // SKU -> ProductInfo
	firstRun          = true                         // Flag to handle initial run
	istLocation       *time.Location                 // For IST timezone handling
	monitoredSKUsMap  map[string]bool                // Set of SKUs to monitor (loaded from env)
	cookieExpiry      time.Time                      // When the current cookie expires
	httpClient        *http.Client                   // Reusable HTTP client with cookie jar
)

var checkInterval, _ = time.ParseDuration("60m")

// --- Telegram Configuration ---
var (
	telegramBotToken string
	telegramChatID   string
)

func main() {
	var err error

	checkIntervalPtr := flag.Duration("check-interval", checkInterval, "interval at which the app will check for stock")
	flag.Parse()
	checkInterval = *checkIntervalPtr

	// --- Load Timezone ---
	istLocation, err = time.LoadLocation(timeZone)
	if err != nil {
		log.Fatalf("❌ Error loading timezone '%s': %v", timeZone, err)
	}
	log.Printf("✅ Timezone '%s' loaded successfully. Current time in IST: %s", timeZone, time.Now().In(istLocation).Format(time.RFC1123))
	log.Printf("Notifications will be suppressed between %d:00 and %d:00 %s.", quietHourStart, quietHourEnd, timeZone)

	// --- Load .env file ---
	log.Println("Attempting to load .env file...")
	cwd, _ := os.Getwd()
	log.Printf("Current working directory: %s", cwd)
	if err := godotenv.Load(); err != nil {
		log.Printf("⚠️ Warning: Error loading .env file: %v. Will rely on environment variables.", err)
	} else {
		log.Println("✅ .env file loaded successfully (if found).")
	}

	// --- Read Environment Variables ---
	telegramBotToken = strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	telegramChatID = strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID"))
	monitoredSKUsRaw := strings.TrimSpace(os.Getenv("MONITORED_SKUS"))

	// --- Validate Configuration ---
	if telegramBotToken == "" || telegramChatID == "" {
		log.Fatalf("❌ Error: TELEGRAM_BOT_TOKEN or TELEGRAM_CHAT_ID is empty. Please set them in your environment or .env file.")
	}
	if monitoredSKUsRaw == "" {
		log.Fatalf("❌ Error: MONITORED_SKUS environment variable is not set or empty. Please provide a comma-separated list of SKUs.")
	}

	// --- Process Monitored SKUs ---
	monitoredSKUsMap = make(map[string]bool)
	skuList := strings.Split(monitoredSKUsRaw, ",")
	validSKUs := []string{}
	for _, sku := range skuList {
		trimmedSku := strings.TrimSpace(sku)
		if trimmedSku != "" {
			monitoredSKUsMap[trimmedSku] = true
			validSKUs = append(validSKUs, trimmedSku)
		}
	}
	if len(monitoredSKUsMap) == 0 {
		log.Fatalf("❌ Error: No valid SKUs found in MONITORED_SKUS environment variable after processing '%s'.", monitoredSKUsRaw)
	}
	log.Printf("✅ Monitoring the following %d SKUs: %s", len(validSKUs), strings.Join(validSKUs, ", "))

	// Log Telegram config details
	log.Printf("Telegram Bot Token Length: %d", len(telegramBotToken))
	if len(telegramBotToken) > 10 {
		log.Printf("Telegram Bot Token Hint: Starts with '%s', ends with '%s'", telegramBotToken[:5], telegramBotToken[len(telegramBotToken)-5:])
	}
	log.Printf("Telegram Chat ID: %s", telegramChatID)

	// --- Initialize HTTP Client with Cookie Jar ---
	initHTTPClient()

	// --- Startup Test Notification ---
	testMessage := fmt.Sprintf("🔄 Amul Stock Notifier started successfully! Monitoring %d SKUs. Quiet hours: %d:00-%d:00 %s.", len(monitoredSKUsMap), quietHourStart, quietHourEnd, timeZone)
	err = sendTelegramNotification(testMessage)
	if err != nil {
		if !isQuietHours(istLocation) {
			log.Fatalf("❌ Failed to send test notification (outside quiet hours): %v. Check Telegram config.", err)
		} else {
			log.Printf("ℹ️ Test notification suppressed due to quiet hours.")
		}
	} else {
		log.Println("✅ Test notification sent successfully (or suppressed due to quiet hours).")
	}

	log.Println("Starting Amul product stock notifier...")

	// --- Initial Check ---
	log.Println("Performing initial stock check to establish baseline...")
	checkTargetStock()
	sendInitialStockNotifications()

	firstRun = false
	log.Println("✅ Initial setup complete. Regular checks starting...")

	// --- Start Regular Checks ---
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for range ticker.C {
		checkTargetStock()
	}
}

func initHTTPClient() {
	jar, err := cookiejar.New(nil)
	if err != nil {
		log.Fatal("Error creating cookie jar:", err)
	}

	httpClient = &http.Client{
		Jar: jar,
	}

	// Get initial cookie
	refreshCookie()
}

func refreshCookie() {
	log.Println("Refreshing Amul API cookie...")

	// First request to get the jsessionid
	targetURL := "https://shop.amul.com/en/"
	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		log.Fatal("Error creating request:", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/58.0.3029.110 Safari/537.3")

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Fatal("Error sending request:", err)
	}
	defer resp.Body.Close()

	// Extract expiration from headers
	for _, headerCookieStr := range resp.Header["Set-Cookie"] {
		parts := strings.Split(headerCookieStr, ";")
		if len(parts) > 0 && strings.HasPrefix(strings.TrimSpace(parts[0]), "jsessionid=") {
			for _, part := range parts {
				trimmedPart := strings.TrimSpace(part)
				if strings.HasPrefix(trimmedPart, "Expires=") {
					expiresStr := strings.TrimPrefix(trimmedPart, "Expires=")
					expiry, err := time.Parse(time.RFC1123, expiresStr)
					if err != nil {
						log.Printf("Warning: Could not parse cookie expiry time: %v", err)
						cookieExpiry = time.Now().Add(24 * time.Hour)
					} else {
						cookieExpiry = expiry
						log.Printf("Cookie expires at: %v", cookieExpiry)
					}
					break
				}
			}
			break
		}
	}

	// Now validate the cookie
	putURL := "https://shop.amul.com/entity/ms.settings/_/setPreferences"
	payload := map[string]map[string]string{
		"data": {
			"store": "gujarat",
		},
	}
	jsonPayload, _ := json.Marshal(payload)

	req, err = http.NewRequest("PUT", putURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		log.Fatal("Error creating PUT request:", err)
	}

	// Set all required headers
	req.Header.Set("accept", "application/json, text/plain, */*")
	req.Header.Set("accept-language", "en-US,en;q=0.9")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("frontend", "1")
	req.Header.Set("origin", "https://shop.amul.com")
	req.Header.Set("priority", "u=1, i")
	req.Header.Set("referer", "https://shop.amul.com/")
	req.Header.Set("sec-ch-ua", `"Chromium";v="135", "Not-A.Brand";v="8"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"Linux"`)
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-site", "same-origin")
	req.Header.Set("user-agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36")

	resp, err = httpClient.Do(req)
	if err != nil {
		log.Fatal("Error sending PUT request:", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Cookie validation failed with status: %d", resp.StatusCode)
	}

	log.Println("✅ Cookie successfully refreshed and validated")
}

func checkCookie() {
	if time.Now().Add(cookieRefreshMargin).After(cookieExpiry) {
		refreshCookie()
	}
}

func checkTargetStock() {
	checkCookie()

	log.Printf("Checking stock for %d monitored products...", len(monitoredSKUsMap))

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		log.Printf("❌ Error creating request: %v", err)
		return
	}

	// Set headers
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:137.0) Gecko/20100101 Firefox/137.0")
	req.Header.Set("Referer", "https://shop.amul.com/")
	req.Header.Set("frontend", "1")
	req.Header.Set("Connection", "keep-alive")

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("❌ Error performing request: %v", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("❌ Error reading response body: %v", err)
		return
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("❌ API returned non-OK status: %s", resp.Status)
		return
	}

	var productList ProductListResponse
	err = json.Unmarshal(body, &productList)
	if err != nil {
		log.Printf("❌ Error parsing JSON response: %v", err)
		return
	}

	log.Printf("Received %d products in API response.", len(productList.Data))

	targetSKUsFoundThisCycle := make(map[string]bool)

	for _, product := range productList.Data {
		if _, isMonitored := monitoredSKUsMap[product.SKU]; isMonitored {
			productDetails[product.SKU] = product
			targetSKUsFoundThisCycle[product.SKU] = true

			currentStockStatus := product.Available == 1
			previousStockStatus, exists := productStockState[product.SKU]

			stockStatusStr := "OUT OF STOCK"
			if currentStockStatus {
				stockStatusStr = "IN STOCK"
			}
			log.Printf("Processing %s (SKU: %s): Status=%s", product.Name, product.SKU, stockStatusStr)

			if currentStockStatus {
				log.Printf("✅ Found IN STOCK: %s (SKU: %s)", product.Name, product.SKU)
				link := ""
				if product.Alias != "" {
					link = fmt.Sprintf("\n\n🔗 <a href=\"%s%s\">View on Amul Shop</a>", productBaseURL, product.Alias)
				}

				message := fmt.Sprintf("✅ <b>Stock Available!</b>\n\nProduct: <b>%s</b>\nStatus: <b>IN STOCK</b>\nQuantity: %d\nSKU: %s%s",
					product.Name, product.InventoryQuantity, product.SKU, link)

				sendNotificationWithRetry(message, product.SKU, "in-stock")
			}

			if !currentStockStatus && exists && previousStockStatus {
				log.Printf("ℹ️ STOCK UPDATE: %s (SKU: %s) changed to OUT OF STOCK", product.Name, product.SKU)
				message := fmt.Sprintf("ℹ️ <b>Stock Update</b>\n\nProduct: <b>%s</b>\nStatus: <b>OUT OF STOCK</b>\nSKU: %s",
					product.Name, product.SKU)
				sendNotificationWithRetry(message, product.SKU, "out-of-stock")
			}

			productStockState[product.SKU] = currentStockStatus
		}
	}

	for sku := range monitoredSKUsMap {
		if !targetSKUsFoundThisCycle[sku] {
			if wasInStock, exists := productStockState[sku]; exists && wasInStock {
				log.Printf("⚠️ WARNING: Monitored SKU %s was NOT found in API response. Assuming OUT OF STOCK.", sku)
				productStockState[sku] = false

				prodInfo, detailsExist := productDetails[sku]
				name := sku
				if detailsExist {
					name = prodInfo.Name
				}

				message := fmt.Sprintf("❓ <b>Stock Update (Not Found)</b>\n\nProduct: <b>%s</b>\nStatus: <b>Assumed OUT OF STOCK</b> (Not in API response)\nSKU: %s", name, sku)
				sendNotificationWithRetry(message, sku, "assumed-out-of-stock")
			} else if !exists {
				log.Printf("INFO: Monitored SKU %s was not found in API response and was not previously tracked. Marking as OUT OF STOCK.", sku)
				productStockState[sku] = false
			} else {
				log.Printf("INFO: Monitored SKU %s was not found in API response (was already recorded as out of stock).", sku)
				productStockState[sku] = false
			}
		}
	}
}

func sendInitialStockNotifications() {
	log.Println("Checking for products already in stock at startup...")

	inStockMessages := []string{}

	for sku := range monitoredSKUsMap {
		if inStock, exists := productStockState[sku]; exists && inStock {
			prodInfo, detailsExist := productDetails[sku]
			name := "Unknown Product"
			alias := ""
			inventory := 0
			if detailsExist {
				name = prodInfo.Name
				alias = prodInfo.Alias
				inventory = prodInfo.InventoryQuantity
			} else {
				log.Printf("⚠️ Warning: Details missing for initially in-stock SKU %s", sku)
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
		fullMessage := "🚨 <b>Initial Stock Alert!</b>\n\nThese monitored products are currently IN STOCK:\n" +
			strings.Join(inStockMessages, "\n")

		err := sendTelegramNotification(fullMessage)
		if err != nil {
			if !isQuietHours(istLocation) {
				log.Printf("❌ Error sending initial stock notification: %v", err)
			}
		} else {
			log.Println("✅ Initial stock notification sent (or suppressed).")
		}
	} else {
		log.Println("No monitored products found in stock at startup.")
	}
}

func isQuietHours(loc *time.Location) bool {
	if loc == nil {
		log.Printf("⚠️ Warning: Time location is nil, cannot check quiet hours. Assuming it's NOT quiet hours.")
		return false
	}
	currentTime := time.Now().In(loc)
	currentHour := currentTime.Hour()
	return currentHour >= quietHourStart && currentHour < quietHourEnd
}

func sendNotificationWithRetry(message, sku, notificationType string) {
	if isQuietHours(istLocation) {
		log.Printf("ℹ️ Notification (%s) for SKU %s suppressed due to quiet hours.", notificationType, sku)
		return
	}

	var notifErr error
	for attempts := 0; attempts < 3; attempts++ {
		notifErr = sendTelegramNotification(message)
		if notifErr == nil {
			log.Printf("✅ Telegram notification (%s) sent successfully for %s (Attempt %d).", notificationType, sku, attempts+1)
			return
		}

		log.Printf("⚠️ Attempt %d: Error sending Telegram notification (%s) for %s: %v",
			attempts+1, notificationType, sku, notifErr)

		if attempts < 2 {
			time.Sleep(2 * time.Second)
		}
	}
	log.Printf("❌ FAILED to send Telegram notification (%s) after 3 attempts for %s", notificationType, sku)
}

func sendTelegramNotification(message string) error {
	if isQuietHours(istLocation) {
		log.Printf("ℹ️ Telegram notification suppressed due to quiet hours (%d:00-%d:00 %s).", quietHourStart, quietHourEnd, timeZone)
		return nil
	}

	if telegramBotToken == "" || telegramChatID == "" {
		log.Println("❌ Error: Attempted to send Telegram notification but token or chat ID is missing.")
		return fmt.Errorf("telegram bot token or chat id is not configured")
	}

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", telegramBotToken)

	payload := map[string]string{
		"chat_id":                  telegramChatID,
		"text":                     message,
		"parse_mode":               "HTML",
		"disable_web_page_preview": "false",
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		log.Printf("❌ Error marshalling telegram payload: %v", err)
		return fmt.Errorf("error marshalling telegram payload: %w", err)
	}
	log.Printf("Attempting to send Telegram payload to chat ID %s...", telegramChatID)

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		log.Printf("❌ Error creating Telegram request: %v", err)
		return fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "AmulStockNotifier/1.2")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("❌ Error sending request to Telegram API: %v", err)
		return fmt.Errorf("error sending request to telegram api: %w", err)
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		log.Printf("❌ Error reading Telegram response body (Status: %s): %v", resp.Status, readErr)
		return fmt.Errorf("error reading telegram response body (status %d): %w", resp.StatusCode, readErr)
	}

	log.Printf("Telegram API response Status: %s", resp.Status)
	if resp.StatusCode != http.StatusOK {
		log.Printf("Telegram API response Body (Error): %s", string(body))
		log.Printf("❌ Error: Telegram API returned non-OK status: %d", resp.StatusCode)
		return fmt.Errorf("telegram api returned status %d: %s", resp.StatusCode, string(body))
	}

	var telegramResponse map[string]any
	if err := json.Unmarshal(body, &telegramResponse); err != nil {
		log.Printf("⚠️ Warning: Could not parse successful Telegram response JSON: %v", err)
	} else {
		if ok, exists := telegramResponse["ok"].(bool); !exists || !ok {
			log.Printf("❌ Error: Telegram API status OK, but response indicates failure: %s", string(body))
			return fmt.Errorf("telegram api reported failure despite 200 OK: %s", string(body))
		}
	}

	log.Printf("✅ Telegram request successful (Status: %s)", resp.Status)
	return nil
}
