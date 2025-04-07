package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
	Available         int    `json:"available"`
	InventoryQuantity int    `json:"inventory_quantity"`
	Price             int    `json:"price"`
}

const (
	// API URL fetching products in the 'protein' category
	apiURL = "https://shop.amul.com/api/1/entity/ms.products?fields[name]=1&fields[brand]=1&fields[categories]=1&fields[collections]=1&fields[alias]=1&fields[sku]=1&fields[price]=1&fields[compare_price]=1&fields[original_price]=1&fields[images]=1&fields[metafields]=1&fields[discounts]=1&fields[catalog_only]=1&fields[is_catalog]=1&fields[seller]=1&fields[available]=1&fields[inventory_quantity]=1&fields[net_quantity]=1&fields[num_reviews]=1&fields[avg_rating]=1&fields[inventory_low_stock_quantity]=1&fields[inventory_allow_out_of_stock]=1&filters[0][field]=categories&filters[0][value][0]=protein&filters[0][operator]=in&facets=true&facetgroup=default_category_facet&limit=32&total=1&start=0"

	checkInterval = 10 * time.Second // Check frequency

	// --- Target Product SKUs ---
	plainLassiSKU         = "LASCP61_30"
	roseLassiSKU          = "LASCP40_30"
	kesarMilkshakeSKU     = "DBDCP42_30"
	blueberryMilkshakeSKU = "DBDCP41_30"
)

// --- Global state to track stock status ---
// map key is SKU, value is boolean (true = in stock, false = out of stock)
var (
	productStockState = make(map[string]bool)
	firstRun          = true // Flag to initialize state without notifying on first check
)

// --- Telegram Configuration ---
var (
	telegramBotToken string
	telegramChatID   string
)

func main() {
	// --- Load .env file ---
	log.Println("Attempting to load .env file...")

	// Get current working directory for debugging context
	cwd, err := os.Getwd()
	if err == nil {
		log.Printf("Current working directory: %s", cwd)
		envPath := filepath.Join(cwd, ".env")
		if _, statErr := os.Stat(envPath); statErr == nil {
			log.Printf(".env file found at: %s", envPath)
		} else {
			if os.IsNotExist(statErr) {
				log.Printf("Warning: .env file was NOT found in the current working directory.")
			} else {
				log.Printf("Warning: Error checking for .env file: %v", statErr)
			}
		}
	} else {
		log.Printf("Warning: Could not get current working directory: %v", err)
		cwd = "[unknown]"
	}

	// Try loading .env
	err = godotenv.Load()
	if err != nil {
		log.Printf("Warning: Error loading .env file: %v. Will try environment variables directly.", err)
	} else {
		log.Println(".env file loaded successfully.")
	}

	// Read environment variables
	telegramBotToken = os.Getenv("TELEGRAM_BOT_TOKEN")
	telegramChatID = os.Getenv("TELEGRAM_CHAT_ID")

	// Trim any potential whitespace from telegram token and chat ID
	telegramBotToken = strings.TrimSpace(telegramBotToken)
	telegramChatID = strings.TrimSpace(telegramChatID)

	log.Printf("TELEGRAM_BOT_TOKEN length: %d", len(telegramBotToken))
	log.Printf("TELEGRAM_CHAT_ID length: %d", len(telegramChatID))

	// Check if variables are set
	if telegramBotToken == "" || telegramChatID == "" {
		log.Fatalf("Error: TELEGRAM_BOT_TOKEN or TELEGRAM_CHAT_ID is empty. Please check your environment variables or .env file.")
	}

	// Print first and last few characters for debugging (don't expose full token)
	if len(telegramBotToken) > 10 {
		log.Printf("Token starts with: %s... and ends with ...%s", telegramBotToken[:5], telegramBotToken[len(telegramBotToken)-5:])
	}

	// Send a test notification at startup
	testMessage := "🔄 Amul Stock Notifier started successfully! This is a test notification."
	err = sendTelegramNotification(testMessage)
	if err != nil {
		log.Fatalf("❌ Failed to send test notification: %v. Please check your Telegram configuration.", err)
	} else {
		log.Println("✅ Test notification sent successfully to Telegram.")
	}

	log.Println("Starting Amul product stock notifier...")
	log.Printf("Monitoring SKUs: %s, %s, %s, %s", plainLassiSKU, roseLassiSKU, kesarMilkshakeSKU, blueberryMilkshakeSKU)
	log.Printf("Notifications will be sent to Telegram Chat ID: %s", telegramChatID)

	// Validate API URL
	if _, urlErr := url.Parse(apiURL); urlErr != nil {
		log.Fatalf("Invalid API URL: %v", urlErr)
	}

	// Perform an initial check immediately without notifications
	log.Println("Performing initial stock check to establish baseline...")
	checkTargetStock()

	// Force initial notification if any products are in stock
	sendInitialStockNotifications()

	// Now set firstRun to false to enable regular notifications
	firstRun = false

	// Set up ticker for regular checks
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	// Continue checking at regular intervals
	for range ticker.C {
		checkTargetStock()
	}
}

// Send notifications for any products that are already in stock at startup
func sendInitialStockNotifications() {
	log.Println("Checking for products already in stock at startup...")

	// Get product names from SKUs (we'll just use placeholder names for now)
	skuToName := map[string]string{
		plainLassiSKU:         "Plain Lassi",
		roseLassiSKU:          "Rose Lassi",
		kesarMilkshakeSKU:     "Kesar Milkshake",
		blueberryMilkshakeSKU: "Blueberry Milkshake",
	}

	// Check each product in our state map
	inStockProducts := []string{}

	for sku, inStock := range productStockState {
		if inStock {
			name := skuToName[sku]
			if name == "" {
				name = "Unknown Product"
			}
			log.Printf("Found product already in stock at startup: %s (SKU: %s)", name, sku)
			inStockProducts = append(inStockProducts, fmt.Sprintf("- %s (SKU: %s)", name, sku))
		}
	}

	// Send a notification if any products are in stock
	if len(inStockProducts) > 0 {
		message := "🚨 <b>Initial Stock Alert!</b>\n\nThese products are currently IN STOCK:\n" +
			strings.Join(inStockProducts, "\n")

		err := sendTelegramNotification(message)
		if err != nil {
			log.Printf("❌ Error sending initial stock notification: %v", err)
		} else {
			log.Println("✅ Initial stock notification sent successfully")
		}
	} else {
		log.Println("No products found in stock at startup.")
	}
}

// Checks stock for target products and handles state/notifications
func checkTargetStock() {
	log.Printf("Checking stock for target products...")

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		log.Printf("Error creating request: %v", err)
		return
	}

	// Set headers
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:137.0) Gecko/20100101 Firefox/137.0")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("Referer", "https://shop.amul.com/")
	req.Header.Set("frontend", "1")
	req.Header.Set("Sec-GPC", "1")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("TE", "trailers")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error performing request: %v", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response body: %v", err)
		return
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("API returned non-OK status: %s", resp.Status)
		return // Don't proceed if API call failed
	}

	var productList ProductListResponse
	err = json.Unmarshal(body, &productList)
	if err != nil {
		log.Printf("Error parsing JSON response: %v", err)
		bodyPreview := string(body)
		if len(bodyPreview) > 500 {
			bodyPreview = bodyPreview[:500] + "..."
		}
		log.Printf("Response body preview: %s", bodyPreview)
		return // Don't proceed if response is invalid
	}

	// Log how many products were found
	log.Printf("Found %d products in the API response", len(productList.Data))

	// Initialize map to track if targets were found in this cycle's response
	targetSKUsFound := map[string]bool{
		plainLassiSKU:         false,
		roseLassiSKU:          false,
		kesarMilkshakeSKU:     false,
		blueberryMilkshakeSKU: false,
	}

	// Create a map to store product names by SKU
	productNameMap := make(map[string]string)
	productInventoryMap := make(map[string]int)

	// IMPORTANT: First pass - just collect all product data without changing state
	for _, product := range productList.Data {
		// Store product names for all products
		if product.Name != "" {
			productNameMap[product.SKU] = product.Name
			productInventoryMap[product.SKU] = product.InventoryQuantity
		}

		// Check if this is one of our target products
		if product.SKU == plainLassiSKU ||
			product.SKU == roseLassiSKU ||
			product.SKU == kesarMilkshakeSKU ||
			product.SKU == blueberryMilkshakeSKU {

			targetSKUsFound[product.SKU] = true

			// Log what we found
			stockStatus := "OUT OF STOCK"
			if product.Available == 1 {
				stockStatus = "IN STOCK"
			}

			log.Printf("Found target product: %s (SKU: %s) - Status: %s, Inventory: %d",
				product.Name, product.SKU, stockStatus, product.InventoryQuantity)
		}
	}

	// SECOND PASS: Now process the products and send notifications if needed
	for _, product := range productList.Data {
		// Check if the current product is one of our targets
		isTarget := false
		if product.SKU == plainLassiSKU ||
			product.SKU == roseLassiSKU ||
			product.SKU == kesarMilkshakeSKU ||
			product.SKU == blueberryMilkshakeSKU {
			isTarget = true
		}

		if isTarget {
			currentStockStatus := product.Available == 1 // true if in stock

			// Get previous status from our state map
			previousStockStatus, exists := productStockState[product.SKU]

			// Debug logging
			log.Printf("Processing %s (SKU: %s): Current Status=%t, Previous Status=%t, First Run=%t",
				product.Name, product.SKU, currentStockStatus, previousStockStatus, firstRun)

			// Check for state change: Out of Stock -> In Stock
			// OR if this is the first time we're seeing this product and it's in stock
			if currentStockStatus && (!exists || !previousStockStatus) && !firstRun {
				log.Printf("🚨 STOCK ALERT: %s is now IN STOCK!", product.Name)

				message := fmt.Sprintf("✅ <b>Stock Alert!</b>\n\nProduct: <b>%s</b>\nStatus: <b>IN STOCK</b>\nQuantity: %d\nSKU: %s\n\n🔗 <a href=\"https://shop.amul.com/products/%s\">View on Amul Shop</a>",
					product.Name, product.InventoryQuantity, product.SKU, product.Alias)

				// Try sending the notification multiple times if it fails
				var notifErr error
				for attempts := 0; attempts < 3; attempts++ {
					notifErr = sendTelegramNotification(message)
					if notifErr == nil {
						log.Printf("✅ Telegram notification sent successfully for %s.", product.SKU)
						break
					}
					log.Printf("⚠️ Attempt %d: Error sending Telegram notification for %s: %v",
						attempts+1, product.SKU, notifErr)
					time.Sleep(2 * time.Second) // Wait before retrying
				}

				if notifErr != nil {
					log.Printf("❌ FAILED to send Telegram notification after 3 attempts for %s", product.SKU)
				}
			}

			// Also check for state change: In Stock -> Out of Stock
			if !currentStockStatus && exists && previousStockStatus {
				log.Printf("ℹ️ STOCK UPDATE: %s is now OUT OF STOCK", product.Name)

				// Optionally send notification for out-of-stock status
				message := fmt.Sprintf("ℹ️ <b>Stock Update</b>\n\nProduct: <b>%s</b>\nStatus: <b>OUT OF STOCK</b>\nSKU: %s",
					product.Name, product.SKU)

				err := sendTelegramNotification(message)
				if err != nil {
					log.Printf("⚠️ Error sending out-of-stock notification: %v", err)
				}
			}

			// Update the state map for the next check
			productStockState[product.SKU] = currentStockStatus
		}
	}

	// Send a daily summary of all stock status (optional)
	hour := time.Now().Hour()
	minute := time.Now().Minute()
	if hour == 9 && minute < 5 { // Around 9:00 AM
		sendDailySummary(productNameMap, productInventoryMap)
	}

	// Log if target SKUs were not found in the API response during this check
	for sku, found := range targetSKUsFound {
		if !found {
			log.Printf("WARNING: Target SKU %s was not found in the API response this cycle.", sku)
		}
	}
}

// Send a daily summary of all product stock status
func sendDailySummary(nameMap map[string]string, inventoryMap map[string]int) {
	log.Println("Sending daily summary of stock status...")

	summary := "<b>📊 Daily Stock Summary</b>\n\n"

	// Add each product to the summary
	targetSKUs := []string{plainLassiSKU, roseLassiSKU, kesarMilkshakeSKU, blueberryMilkshakeSKU}

	for _, sku := range targetSKUs {
		name := nameMap[sku]
		if name == "" {
			name = "Unknown Product"
		}

		status := "❌ OUT OF STOCK"
		inventory := 0

		if inStock, exists := productStockState[sku]; exists && inStock {
			status = "✅ IN STOCK"
			inventory = inventoryMap[sku]
		}

		summary += fmt.Sprintf("• <b>%s</b> (SKU: %s): %s", name, sku, status)
		if inventory > 0 {
			summary += fmt.Sprintf(" (%d available)", inventory)
		}
		summary += "\n"
	}

	// Send the summary
	err := sendTelegramNotification(summary)
	if err != nil {
		log.Printf("❌ Error sending daily summary: %v", err)
	} else {
		log.Println("✅ Daily summary sent successfully")
	}
}

// Function to send message via Telegram Bot API
func sendTelegramNotification(message string) error {
	if telegramBotToken == "" || telegramChatID == "" {
		return fmt.Errorf("telegram bot token or chat id is not configured")
	}

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", telegramBotToken)

	// Log the Telegram API URL being used (partial, for security)
	urlParts := strings.Split(apiURL, telegramBotToken)
	if len(urlParts) >= 2 {
		log.Printf("Using Telegram API URL: %s[TOKEN]%s", urlParts[0], urlParts[1])
	}

	payload := map[string]string{
		"chat_id":                  telegramChatID,
		"text":                     message,
		"parse_mode":               "HTML",  // Enable HTML formatting
		"disable_web_page_preview": "false", // Allow web page previews for links
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("error marshalling telegram payload: %w", err)
	}

	// Debug: Print the payload we're sending (redacting sensitive info)
	payloadStr := string(jsonPayload)
	payloadStr = strings.Replace(payloadStr, telegramBotToken, "[TOKEN]", -1)
	payloadStr = strings.Replace(payloadStr, telegramChatID, "[CHAT_ID]", -1)
	log.Printf("Sending Telegram payload: %s", payloadStr)

	// Create a custom HTTP client with timeout
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Create request manually to add headers
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "AmulStockNotifier/1.0")

	// Perform the request
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error sending request to telegram api: %w", err)
	}
	defer resp.Body.Close()

	// Read and log the full response for debugging
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return fmt.Errorf("error reading telegram response body: %w", readErr)
	}

	log.Printf("Telegram API response status: %s", resp.Status)
	log.Printf("Telegram API response body: %s", string(body))

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram api returned status %d: %s", resp.StatusCode, string(body))
	}

	// Validate the response indicates success
	var telegramResponse map[string]any
	if err := json.Unmarshal(body, &telegramResponse); err != nil {
		return fmt.Errorf("error parsing telegram response: %w", err)
	}

	// Check if the 'ok' field is true
	if ok, exists := telegramResponse["ok"].(bool); !exists || !ok {
		return fmt.Errorf("telegram api reported failure: %s", string(body))
	}

	return nil // Success
}
