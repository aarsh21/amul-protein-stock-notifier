package bot

import (
	"amul-notifier/internal/config"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"strings"
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
	}, nil
}


func checkCookie(cookieExpiry time.Time, botHttpClient *http.Client) {
	if time.Now().Add(cookieRefreshMargin).After(cookieExpiry) {
		refreshCookie(botHttpClient)
	}
}

func CheckTargetStock(bot *Bot) {
	checkCookie(bot.cookieExpiry, bot.httpClient)

	log.Printf("Checking stock for %d monitored products...", len(bot.appConfig.MonitoredSKUsMap))

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
		if _, isMonitored := bot.appConfig.MonitoredSKUsMap[product.SKU]; isMonitored {
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
				link := ""
				if product.Alias != "" {
					link = fmt.Sprintf("\n\n🔗 <a href=\"%s%s\">View on Amul Shop</a>", productBaseURL, product.Alias)
				}

				message := fmt.Sprintf("✅ <b>Stock Available!</b>\n\nProduct: <b>%s</b>\nStatus: <b>IN STOCK</b>\nQuantity: %d\nSKU: %s%s",
					product.Name, product.InventoryQuantity, product.SKU, link)

				sendNotificationWithRetry(bot.appConfig, message, product.SKU, "in-stock")
			}

			if !currentStockStatus && exists && previousStockStatus {
				log.Printf("ℹ️ STOCK UPDATE: %s (SKU: %s) changed to OUT OF STOCK", product.Name, product.SKU)
				message := fmt.Sprintf("ℹ️ <b>Stock Update</b>\n\nProduct: <b>%s</b>\nStatus: <b>OUT OF STOCK</b>\nSKU: %s",
					product.Name, product.SKU)
				sendNotificationWithRetry(bot.appConfig, message, product.SKU, "out-of-stock")
			}

			bot.productStockState[product.SKU] = currentStockStatus
		}
	}

	for sku := range bot.appConfig.MonitoredSKUsMap {
		if !targetSKUsFoundThisCycle[sku] {
			if wasInStock, exists := bot.productStockState[sku]; exists && wasInStock {
				log.Printf("WARNING: Monitored SKU %s was NOT found in API response. Assuming OUT OF STOCK.", sku)
				bot.productStockState[sku] = false

				prodInfo, detailsExist := bot.productDetails[sku]
				name := sku
				if detailsExist {
					name = prodInfo.Name
				}

				message := fmt.Sprintf("<b>Stock Update (Not Found)</b>\n\nProduct: <b>%s</b>\nStatus: <b>Assumed OUT OF STOCK</b> (Not in API response)\nSKU: %s", name, sku)
				sendNotificationWithRetry(bot.appConfig, message, sku, "assumed-out-of-stock")
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

func refreshCookie(httpClient *http.Client) (time.Time, error) {
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
			"store": "gujarat",
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
