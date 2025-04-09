package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

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
	// Consider making the category filter configurable too if you monitor other types later
	apiURL = "https://shop.amul.com/api/1/entity/ms.products?fields[name]=1&fields[brand]=1&fields[categories]=1&fields[collections]=1&fields[alias]=1&fields[sku]=1&fields[price]=1&fields[compare_price]=1&fields[original_price]=1&fields[images]=1&fields[metafields]=1&fields[discounts]=1&fields[catalog_only]=1&fields[is_catalog]=1&fields[seller]=1&fields[available]=1&fields[inventory_quantity]=1&fields[net_quantity]=1&fields[num_reviews]=1&fields[avg_rating]=1&fields[inventory_low_stock_quantity]=1&fields[inventory_allow_out_of_stock]=1&filters[0][field]=categories&filters[0][value][0]=protein&filters[0][operator]=in&facets=true&facetgroup=default_category_facet&limit=100&total=1&start=0" // Increased limit a bit

	// *** CHANGED BASE URL ***
	productBaseURL = "https://shop.amul.com/en/product/"

	// --- Quiet Hours Configuration (IST) ---
	quietHourStart = 0 // 12:00 AM
	quietHourEnd   = 7 // Up to 6:59:59 AM (exclusive of 7)
	timeZone       = "Asia/Kolkata"
)

// --- Global state ---
var (
	productStockState = make(map[string]bool)        // SKU -> inStock (bool)
	productDetails    = make(map[string]ProductInfo) // SKU -> ProductInfo
	firstRun          = true                         // Flag to handle initial run
	istLocation       *time.Location                 // For IST timezone handling
	monitoredSKUsMap  map[string]bool                // Set of SKUs to monitor (loaded from env)
)
var checkInterval, _ = time.ParseDuration("60m")

// --- Telegram Configuration ---
var (
	telegramBotToken string
	telegramChatID   string
)

func main() {
	var err error // Declare err here for timezone loading

	checkIntervalPtr := flag.Duration("check-interval", checkInterval, "interval at which the app will check for stock")
	flag.Parse()
	checkInterval = *checkIntervalPtr;

	// --- Load Timezone ---
	istLocation, err = time.LoadLocation(timeZone)
	if err != nil {
		log.Fatalf("❌ Error loading timezone '%s': %v", timeZone, err)
	}
	log.Printf("✅ Timezone '%s' loaded successfully. Current time in IST: %s", timeZone, time.Now().In(istLocation).Format(time.RFC1123))
	log.Printf("Notifications will be suppressed between %d:00 and %d:00 %s.", quietHourStart, quietHourEnd, timeZone)

	// --- Load .env file ---
	log.Println("Attempting to load .env file...")
	// (Keep the existing .env loading and debugging logic)
	cwd, _ := os.Getwd() // Ignore error for logging purpose
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
			validSKUs = append(validSKUs, trimmedSku) // Keep a list for logging
		}
	}
	if len(monitoredSKUsMap) == 0 {
		log.Fatalf("❌ Error: No valid SKUs found in MONITORED_SKUS environment variable after processing '%s'.", monitoredSKUsRaw)
	}
	log.Printf("✅ Monitoring the following %d SKUs: %s", len(validSKUs), strings.Join(validSKUs, ", "))

	// Log Telegram config details (partially)
	log.Printf("Telegram Bot Token Length: %d", len(telegramBotToken))
	if len(telegramBotToken) > 10 {
		log.Printf("Telegram Bot Token Hint: Starts with '%s', ends with '%s'", telegramBotToken[:5], telegramBotToken[len(telegramBotToken)-5:])
	}
	log.Printf("Telegram Chat ID: %s", telegramChatID)

	// --- Startup Test Notification ---
	testMessage := fmt.Sprintf("🔄 Amul Stock Notifier started successfully! Monitoring %d SKUs. Quiet hours: %d:00-%d:00 %s.", len(monitoredSKUsMap), quietHourStart, quietHourEnd, timeZone)
	// Send test notification (respecting quiet hours)
	err = sendTelegramNotification(testMessage)
	if err != nil {
		// Log fatal only if sending failed *outside* quiet hours
		if !isQuietHours(istLocation) {
			log.Fatalf("❌ Failed to send test notification (outside quiet hours): %v. Check Telegram config.", err)
		} else {
			log.Printf("ℹ️ Test notification suppressed due to quiet hours.")
		}
	} else {
		log.Println("✅ Test notification sent successfully (or suppressed due to quiet hours).")
	}

	log.Println("Starting Amul product stock notifier...")

	// Validate API URL
	if _, urlErr := url.Parse(apiURL); urlErr != nil {
		log.Fatalf("❌ Invalid API URL: %v", urlErr)
	}

	// --- Initial Check ---
	log.Println("Performing initial stock check to establish baseline...")
	checkTargetStock() // Populates productDetails and initial productStockState

	// Send notifications for products already in stock at startup (respecting quiet hours)
	sendInitialStockNotifications()

	firstRun = false // Enable regular notifications from now on
	log.Println("✅ Initial setup complete. Regular checks starting...")

	// --- Start Regular Checks ---
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for range ticker.C {
		checkTargetStock()
	}
}

// Checks if the current time is within the defined quiet hours in the given location
func isQuietHours(loc *time.Location) bool {
	if loc == nil {
		log.Printf("⚠️ Warning: Time location is nil, cannot check quiet hours. Assuming it's NOT quiet hours.")
		return false // Fail safe: don't suppress if location isn't loaded
	}
	currentTime := time.Now().In(loc)
	currentHour := currentTime.Hour()
	// Check if current hour is within the range [quietHourStart, quietHourEnd)
	isQuiet := currentHour >= quietHourStart && currentHour < quietHourEnd
	// Optional: Log when check occurs during quiet hours
	// if isQuiet {
	//  log.Printf("DEBUG: Currently within quiet hours (%d:00-%d:00 %s). Current hour: %d", quietHourStart, quietHourEnd, loc.String(), currentHour)
	// }
	return isQuiet
}

// Send notifications for any products that are already in stock at startup
func sendInitialStockNotifications() {
	log.Println("Checking for products already in stock at startup...")

	inStockMessages := []string{}

	// Check each monitored product's initial state
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

			// Format message per product
			message := fmt.Sprintf("• <b>%s</b> (SKU: %s) - Qty: %d %s", name, sku, inventory, link)
			inStockMessages = append(inStockMessages, message)
		}
	}

	// Send a combined notification if any products are in stock
	if len(inStockMessages) > 0 {
		fullMessage := "🚨 <b>Initial Stock Alert!</b>\n\nThese monitored products are currently IN STOCK:\n" +
			strings.Join(inStockMessages, "\n")

		err := sendTelegramNotification(fullMessage) // This will respect quiet hours
		if err != nil {
			// Log error only if it happened outside quiet hours
			if !isQuietHours(istLocation) {
				log.Printf("❌ Error sending initial stock notification: %v", err)
			}
			// If it was quiet hours, sendTelegramNotification already logged suppression
		} else {
			log.Println("✅ Initial stock notification sent (or suppressed).")
		}
	} else {
		log.Println("No monitored products found in stock at startup.")
	}
}

// Checks stock for target products and handles state/notifications
func checkTargetStock() {
	isScheduledCheck := !firstRun
	if isScheduledCheck {
		log.Printf("Checking stock for %d monitored products (Scheduled Check)...", len(monitoredSKUsMap))
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		log.Printf("❌ Error creating request: %v", err)
		return
	}

	// Set headers (keep existing headers)
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:137.0) Gecko/20100101 Firefox/137.0")
	// ... other headers ...
	req.Header.Set("Referer", "https://shop.amul.com/")
	req.Header.Set("frontend", "1")
	req.Header.Set("Connection", "keep-alive")

	resp, err := client.Do(req)
	// (Keep existing request execution and error handling)
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
		// (Keep body preview logging on error)
		return
	}

	var productList ProductListResponse
	err = json.Unmarshal(body, &productList)
	// (Keep existing JSON parsing and error handling)
	if err != nil {
		log.Printf("❌ Error parsing JSON response: %v", err)
		// (Keep body preview logging on error)
		return
	}

	if isScheduledCheck {
		log.Printf("Received %d products in API response for the query.", len(productList.Data))
	}

	// --- Update Product Details and Check Stock ---
	targetSKUsFoundThisCycle := make(map[string]bool)

	for _, product := range productList.Data {
		// Check if this product is one we are monitoring
		if _, isMonitored := monitoredSKUsMap[product.SKU]; isMonitored {
			// Update details for this monitored product
			productDetails[product.SKU] = product
			targetSKUsFoundThisCycle[product.SKU] = true

			currentStockStatus := product.Available == 1
			previousStockStatus, exists := productStockState[product.SKU]

			if isScheduledCheck {
				stockStatusStr := "OUT OF STOCK"
				if currentStockStatus {
					stockStatusStr = "IN STOCK"
				}
				log.Printf("Processing %s (SKU: %s): API Status=%s (Avail=%d, Qty=%d), Recorded Status=%t",
					product.Name, product.SKU, stockStatusStr, product.Available, product.InventoryQuantity, previousStockStatus)
			}

			// --- Notification Logic ---

			// 1. Notify if IN STOCK during a scheduled check
			if currentStockStatus && isScheduledCheck {
				log.Printf("✅ Found IN STOCK during scheduled check: %s (SKU: %s)", product.Name, product.SKU)
				link := ""
				if product.Alias != "" {
					link = fmt.Sprintf("\n\n🔗 <a href=\"%s%s\">View on Amul Shop</a>", productBaseURL, product.Alias)
				} else {
					log.Printf("⚠️ Warning: Alias is empty for SKU %s, cannot generate link.", product.SKU)
				}
				message := fmt.Sprintf("✅ <b>Stock Available!</b>\n\nProduct: <b>%s</b>\nStatus: <b>IN STOCK</b>\nQuantity: %d\nSKU: %s%s",
					product.Name, product.InventoryQuantity, product.SKU, link)

				sendNotificationWithRetry(message, product.SKU, "in-stock") // Will respect quiet hours
			}

			// 2. Notify if status changed from IN STOCK -> OUT OF STOCK
			if !currentStockStatus && exists && previousStockStatus && isScheduledCheck {
				log.Printf("ℹ️ STOCK UPDATE: %s (SKU: %s) changed to OUT OF STOCK", product.Name, product.SKU)
				message := fmt.Sprintf("ℹ️ <b>Stock Update</b>\n\nProduct: <b>%s</b>\nStatus: <b>OUT OF STOCK</b>\nSKU: %s",
					product.Name, product.SKU)
				sendNotificationWithRetry(message, product.SKU, "out-of-stock") // Will respect quiet hours
			}

			// Update the state map *after* processing notifications
			productStockState[product.SKU] = currentStockStatus
		}
	}

	// --- Check if any monitored SKUs were missing from the response ---
	if isScheduledCheck {
		for sku := range monitoredSKUsMap {
			if !targetSKUsFoundThisCycle[sku] {
				// If it was previously in stock or state exists, mark as out of stock
				if wasInStock, exists := productStockState[sku]; exists && wasInStock {
					log.Printf("⚠️ WARNING: Monitored SKU %s was NOT found in API response. Assuming OUT OF STOCK.", sku)
					productStockState[sku] = false // Mark as out of stock

					prodInfo, detailsExist := productDetails[sku] // Get last known name
					name := sku                                   // Default to SKU
					if detailsExist {
						name = prodInfo.Name
					}

					message := fmt.Sprintf("❓ <b>Stock Update (Not Found)</b>\n\nProduct: <b>%s</b>\nStatus: <b>Assumed OUT OF STOCK</b> (Not in API response)\nSKU: %s", name, sku)
					sendNotificationWithRetry(message, sku, "assumed-out-of-stock") // Respects quiet hours

				} else if !exists {
					// If it was never seen before (doesn't exist in state map), mark as out of stock
					log.Printf("INFO: Monitored SKU %s was not found in API response and was not previously tracked. Marking as OUT OF STOCK.", sku)
					productStockState[sku] = false
				} else {
					// If it was already out of stock, just log INFO
					log.Printf("INFO: Monitored SKU %s was not found in API response (was already recorded as out of stock).", sku)
					// Ensure state remains false
					productStockState[sku] = false
				}
			}
		}
	}

	// Optional: Daily summary logic would go here
	// sendDailySummaryIfNeeded()
}

// Helper function to send notification with retries (respects quiet hours via sendTelegramNotification)
func sendNotificationWithRetry(message, sku, notificationType string) {
	// Initial check for quiet hours before attempting retries
	if isQuietHours(istLocation) {
		log.Printf("ℹ️ Notification (%s) for SKU %s suppressed due to quiet hours.", notificationType, sku)
		return // Don't even attempt to send
	}

	var notifErr error
	for attempts := 0; attempts < 3; attempts++ {
		notifErr = sendTelegramNotification(message) // This function now checks quiet hours internally too, but the check above prevents needless attempts.
		if notifErr == nil {
			// If sendTelegramNotification returns nil, it means success OR suppression.
			// Since we checked isQuietHours above, nil here means success.
			log.Printf("✅ Telegram notification (%s) sent successfully for %s (Attempt %d).", notificationType, sku, attempts+1)
			return // Success
		}

		// If error is not nil, it means sending actually failed (not suppressed)
		log.Printf("⚠️ Attempt %d: Error sending Telegram notification (%s) for %s: %v",
			attempts+1, notificationType, sku, notifErr)

		if attempts < 2 {
			time.Sleep(2 * time.Second)
		}
	}
	log.Printf("❌ FAILED to send Telegram notification (%s) after 3 attempts for %s", notificationType, sku)
}

// Function to send message via Telegram Bot API (Now checks quiet hours)
func sendTelegramNotification(message string) error {
	// *** ADDED: Check for quiet hours before sending ***
	if isQuietHours(istLocation) {
		log.Printf("ℹ️ Telegram notification suppressed due to quiet hours (%d:00-%d:00 %s).", quietHourStart, quietHourEnd, timeZone)
		return nil // Return success (nil error) to indicate suppression, not failure
	}

	if telegramBotToken == "" || telegramChatID == "" {
		log.Println("❌ Error: Attempted to send Telegram notification but token or chat ID is missing.")
		return fmt.Errorf("telegram bot token or chat id is not configured")
	}

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", telegramBotToken)
	// (Keep existing URL logging)

	payload := map[string]string{
		"chat_id":                  telegramChatID,
		"text":                     message,
		"parse_mode":               "HTML",
		"disable_web_page_preview": "false",
	}

	jsonPayload, err := json.Marshal(payload)
	// (Keep existing payload logging and marshalling error handling)
	if err != nil {
		log.Printf("❌ Error marshalling telegram payload: %v", err)
		return fmt.Errorf("error marshalling telegram payload: %w", err)
	}
	log.Printf("Attempting to send Telegram payload to chat ID %s...", telegramChatID) // Removed payload content log

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonPayload))
	// (Keep existing request creation and header setting)
	if err != nil {
		log.Printf("❌ Error creating Telegram request: %v", err)
		return fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "AmulStockNotifier/1.2")

	resp, err := client.Do(req)
	// (Keep existing request execution and error handling)
	if err != nil {
		log.Printf("❌ Error sending request to Telegram API: %v", err)
		return fmt.Errorf("error sending request to telegram api: %w", err)
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	// (Keep existing body reading and error handling)
	if readErr != nil {
		log.Printf("❌ Error reading Telegram response body (Status: %s): %v", resp.Status, readErr)
		return fmt.Errorf("error reading telegram response body (status %d): %w", resp.StatusCode, readErr)
	}

	log.Printf("Telegram API response Status: %s", resp.Status)
	// Only log body on non-OK status for cleaner logs during normal operation
	if resp.StatusCode != http.StatusOK {
		log.Printf("Telegram API response Body (Error): %s", string(body))
		log.Printf("❌ Error: Telegram API returned non-OK status: %d", resp.StatusCode)
		return fmt.Errorf("telegram api returned status %d: %s", resp.StatusCode, string(body))
	}

	// Optional: Validate success response body (keep existing logic)
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
	return nil // Success
}

// Placeholder for daily summary function (if needed later)
// func sendDailySummaryIfNeeded() {
//     if isQuietHours(istLocation) { return } // Respect DND
//     // Add logic to check time of day, e.g., once around 9 AM IST
//     // Use monitoredSKUsMap, productDetails, productStockState
// }
