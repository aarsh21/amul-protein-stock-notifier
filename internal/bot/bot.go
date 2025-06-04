package bot

import (
	"amul-notifier/internal/config"
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"time"
)

const (
	// API URL fetching products in the 'protein' category (adjust filters if needed)
	apiURL = "https://shop.amul.com/api/1/entity/ms.products?fields[name]=1&fields[brand]=1&fields[categories]=1&fields[collections]=1&fields[alias]=1&fields[sku]=1&fields[price]=1&fields[compare_price]=1&fields[original_price]=1&fields[images]=1&fields[metafields]=1&fields[discounts]=1&fields[catalog_only]=1&fields[is_catalog]=1&fields[seller]=1&fields[available]=1&fields[inventory_quantity]=1&fields[net_quantity]=1&fields[num_reviews]=1&fields[avg_rating]=1&fields[inventory_low_stock_quantity]=1&fields[inventory_allow_out_of_stock]=1&filters[0][field]=categories&filters[0][value][0]=protein&filters[0][operator]=in&facets=true&facetgroup=default_category_facet&limit=100&total=1&start=0"

	productBaseURL = "https://shop.amul.com/en/product/"

	// TODO: configure quiet hours
	quietHourStart = 0 // 12:00 AM
	quietHourEnd   = 7 // Up to 6:59:59 AM (exclusive of 7)

	// TODO: parse the expiry time and generate one more cookie again
	cookieRefreshMargin = 90 * time.Hour // Refresh cookie before it expires
)

// isQuietHours checks if the current time is within quiet hours
func isQuietHours(loc *time.Location) bool {
	if loc == nil {
		log.Printf("Warning: Time location is nil, cannot check quiet hours. Assuming it's NOT quiet hours.")
		return false
	}
	currentTime := time.Now().In(loc)
	currentHour := currentTime.Hour()
	return currentHour >= quietHourStart && currentHour < quietHourEnd
}

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

type Bot struct {
	// SKU -> inStock (bool)
	productStockState map[string]bool

	// SKU -> ProductInfo
	productDetails map[string]ProductInfo

	firstRun bool

	// When the current cookie expires
	cookieExpiry time.Time

	// Reusable HTTP client with cookie jar
	httpClient *http.Client

	appConfig *config.AppConfig

	// Interactive bot for user management
	interactiveBot *InteractiveBot

	// Rate limiting
	lastAPICall    time.Time
	apiCallMutex   sync.Mutex
	rateLimitDelay time.Duration
}

func SetBotFirstRun(bot *Bot) {
	bot.firstRun = true
}

func InitBot(appConfig *config.AppConfig) (*Bot, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	httpClient := &http.Client{
		Jar: jar,
	}

	cookieExpiry, err := refreshCookie(httpClient)
	if err != nil {
		return nil, err
	}
	return &Bot{
		productStockState: make(map[string]bool),
		productDetails:    make(map[string]ProductInfo),
		httpClient:        httpClient,
		cookieExpiry:      cookieExpiry,
		appConfig:         appConfig,
		rateLimitDelay:    5 * time.Second, // 5 second delay between API calls
	}, nil
}

func (bot *Bot) SetInteractiveBot(interactiveBot *InteractiveBot) {
	bot.interactiveBot = interactiveBot
}

// GetUserStorePreference returns the store preference for API calls
// It tries to get a user's store preference, fallback to "gujarat"
func (bot *Bot) GetUserStorePreference() string {
	if bot.interactiveBot != nil {
		// Get the first user's store preference as a representative
		// In a real scenario, you might want to use the most common preference
		// or make API calls per user, but for simplicity we'll use the first one
		users := bot.interactiveBot.GetUserSubscriptions()
		for _, user := range users {
			if user.StoreCode != "" {
				log.Printf("Using store preference: %s", user.StoreCode)
				return user.StoreCode
			}
		}
	}

	log.Printf("Using default store preference: gujarat")
	return "gujarat" // default fallback
}

// checkSpecificSKUs checks stock for specific SKUs and returns results
func (bot *Bot) checkSpecificSKUs(targetSKUs map[string]bool) map[string]ProductInfo {
	results := make(map[string]ProductInfo)

	if len(targetSKUs) == 0 {
		return results
	}

	log.Printf("Performing manual stock check for %d SKUs...", len(targetSKUs))

	// Enforce rate limiting
	bot.enforceRateLimit()

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		log.Printf("Error creating request for manual check: %v", err)
		return results
	}

	// Set headers
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:137.0) Gecko/20100101 Firefox/137.0")
	req.Header.Set("Referer", "https://shop.amul.com/")
	req.Header.Set("frontend", "1")
	req.Header.Set("Connection", "keep-alive")

	resp, err := bot.httpClient.Do(req)
	if err != nil {
		log.Printf("Error performing manual check request: %v", err)
		return results
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading manual check response body: %v", err)
		return results
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("Manual check API returned non-OK status: %s", resp.Status)
		return results
	}

	var productList ProductListResponse
	err = json.Unmarshal(body, &productList)
	if err != nil {
		log.Printf("Error parsing manual check JSON response: %v", err)
		return results
	}

	log.Printf("Manual check: Received %d products in API response", len(productList.Data))

	// Filter for target SKUs
	for _, product := range productList.Data {
		if targetSKUs[product.SKU] {
			results[product.SKU] = product
			log.Printf("Manual check: Found %s (SKU: %s) - Available: %d", product.Name, product.SKU, product.Available)
		}
	}

	log.Printf("Manual check completed: Found %d/%d target products", len(results), len(targetSKUs))
	return results
}

// enforceRateLimit ensures we don't make API calls too frequently
func (bot *Bot) enforceRateLimit() {
	bot.apiCallMutex.Lock()
	defer bot.apiCallMutex.Unlock()

	timeSinceLastCall := time.Since(bot.lastAPICall)
	if timeSinceLastCall < bot.rateLimitDelay {
		sleepDuration := bot.rateLimitDelay - timeSinceLastCall
		log.Printf("⏱️ Rate limiting: Waiting %v before next API call", sleepDuration)
		time.Sleep(sleepDuration)
	}

	bot.lastAPICall = time.Now()
}

func (bot *Bot) checkCookie() {
	if time.Now().Add(cookieRefreshMargin).After(bot.cookieExpiry) {
		newExpiry, err := bot.refreshCookie()
		if err != nil {
			log.Printf("Error refreshing cookie: %v", err)
		} else {
			bot.cookieExpiry = newExpiry
		}
	}
}

func CheckTargetStock(bot *Bot) {
	bot.checkCookie()

	// Enforce rate limiting
	bot.enforceRateLimit()

	// Get all subscribed SKUs from interactive bot if available
	var monitoredSKUs map[string]bool
	if bot.interactiveBot != nil {
		monitoredSKUs = bot.interactiveBot.GetAllSubscribedSKUs()
		if len(monitoredSKUs) == 0 {
			log.Println("ℹ️ No users subscribed to any products yet")
			return
		}
	} else {
		monitoredSKUs = bot.appConfig.MonitoredSKUsMap
	}

	log.Printf("Checking stock for %d monitored products...", len(monitoredSKUs))

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		log.Printf("Error creating request: %v", err)
		return
	}

	// Set headers
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:137.0) Gecko/20100101 Firefox/137.0")
	req.Header.Set("Referer", "https://shop.amul.com/")
	req.Header.Set("frontend", "1")
	req.Header.Set("Connection", "keep-alive")

	resp, err := bot.httpClient.Do(req)
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
		return
	}

	var productList ProductListResponse
	err = json.Unmarshal(body, &productList)
	if err != nil {
		log.Printf("Error parsing JSON response: %v", err)
		return
	}

	log.Printf("Received %d products in API response.", len(productList.Data))

	targetSKUsFoundThisCycle := make(map[string]bool)

	for _, product := range productList.Data {
		if _, isMonitored := monitoredSKUs[product.SKU]; isMonitored {
			bot.productDetails[product.SKU] = product
			targetSKUsFoundThisCycle[product.SKU] = true

			currentStockStatus := product.Available == 1
			previousStockStatus, exists := bot.productStockState[product.SKU]

			stockStatusStr := "OUT OF STOCK"
			if currentStockStatus {
				stockStatusStr = "IN STOCK"
			}
			log.Printf("Processing %s (SKU: %s): Status=%s", product.Name, product.SKU, stockStatusStr)

			if currentStockStatus {
				log.Printf("Found IN STOCK: %s (SKU: %s)", product.Name, product.SKU)

				// Send notification through interactive bot
				if bot.interactiveBot != nil {
					bot.interactiveBot.SendStockNotificationToSubscribers(product.SKU, product, true)
				}
			}

			if !currentStockStatus && exists && previousStockStatus {
				log.Printf("ℹ️ STOCK UPDATE: %s (SKU: %s) changed to OUT OF STOCK", product.Name, product.SKU)

				// Send notification through interactive bot
				if bot.interactiveBot != nil {
					bot.interactiveBot.SendStockNotificationToSubscribers(product.SKU, product, false)
				}
			}

			bot.productStockState[product.SKU] = currentStockStatus
		}
	}

	for sku := range monitoredSKUs {
		if !targetSKUsFoundThisCycle[sku] {
			if wasInStock, exists := bot.productStockState[sku]; exists && wasInStock {
				log.Printf("WARNING: Monitored SKU %s was NOT found in API response. Assuming OUT OF STOCK.", sku)
				bot.productStockState[sku] = false

				prodInfo, detailsExist := bot.productDetails[sku]
				name := sku
				if detailsExist {
					name = prodInfo.Name
				}

				// Send notification through interactive bot
				if bot.interactiveBot != nil {
					// Create a dummy product info for the notification
					dummyProductInfo := ProductInfo{
						SKU:  sku,
						Name: name,
					}
					bot.interactiveBot.SendStockNotificationToSubscribers(sku, dummyProductInfo, false)
				}
			} else if !exists {
				log.Printf("INFO: Monitored SKU %s was not found in API response and was not previously tracked. Marking as OUT OF STOCK.", sku)
				bot.productStockState[sku] = false
			} else {
				log.Printf("INFO: Monitored SKU %s was not found in API response (was already recorded as out of stock).", sku)
				bot.productStockState[sku] = false
			}
		}
	}
}

func (bot *Bot) refreshCookie() (time.Time, error) {
	return refreshCookieWithStore(bot.httpClient, bot.GetUserStorePreference())
}

func refreshCookie(httpClient *http.Client) (time.Time, error) {
	return refreshCookieWithStore(httpClient, "gujarat")
}

func refreshCookieWithStore(httpClient *http.Client, storeCode string) (time.Time, error) {
	log.Println("Refreshing Amul API cookie...")

	var cookieExpiry time.Time

	// First request to get the jsessionid
	targetURL := "https://shop.amul.com/en/"
	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		return cookieExpiry, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/58.0.3029.110 Safari/537.3")

	resp, err := httpClient.Do(req)
	if err != nil {
		return cookieExpiry, err
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
			"store": storeCode,
		},
	}
	jsonPayload, _ := json.Marshal(payload)

	req, err = http.NewRequest("PUT", putURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return cookieExpiry, err
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
		return cookieExpiry, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Cookie validation failed with status: %d", resp.StatusCode)
	}

	log.Println("Cookie successfully refreshed and validated")
	return cookieExpiry, nil
}
