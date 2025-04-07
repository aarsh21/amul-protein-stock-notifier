package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings" // Import strings package for Join
	"time"
)

// Struct to match the overall JSON response structure
type ProductListResponse struct {
	Data []ProductInfo `json:"data"`
	// Include other top-level fields if needed, e.g., paging info
}

// Struct for individual product information within the Data array
type ProductInfo struct {
	ID                string `json:"_id"`
	Name              string `json:"name"`
	Alias             string `json:"alias"`
	SKU               string `json:"sku"`
	Available         int    `json:"available"` // 1 if available, 0 otherwise
	InventoryQuantity int    `json:"inventory_quantity"`
	Price             int    `json:"price"`
	// Add other fields if you want to display them
}

const (
	// API URL fetching products in the 'protein' category
	apiURL = "https://shop.amul.com/api/1/entity/ms.products?fields[name]=1&fields[brand]=1&fields[categories]=1&fields[collections]=1&fields[alias]=1&fields[sku]=1&fields[price]=1&fields[compare_price]=1&fields[original_price]=1&fields[images]=1&fields[metafields]=1&fields[discounts]=1&fields[catalog_only]=1&fields[is_catalog]=1&fields[seller]=1&fields[available]=1&fields[inventory_quantity]=1&fields[net_quantity]=1&fields[num_reviews]=1&fields[avg_rating]=1&fields[inventory_low_stock_quantity]=1&fields[inventory_allow_out_of_stock]=1&filters[0][field]=categories&filters[0][value][0]=protein&filters[0][operator]=in&facets=true&facetgroup=default_category_facet&limit=32&total=1&start=0"

	checkInterval = 5 * time.Minute // Check every 15 minutes (adjust as needed)
)

func main() {
	log.Println("Starting Amul product stock lister (Category Mode)...")

	// Validate URL (optional but good practice)
	if _, err := url.Parse(apiURL); err != nil {
		log.Fatalf("Invalid API URL: %v", err)
	}

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	// Perform an initial check immediately
	listProductStock()

	// Continue checking at regular intervals
	for range ticker.C {
		listProductStock()
	}
}

func listProductStock() {
	log.Println("Fetching product list and checking stock status...")

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		log.Printf("Error creating request: %v", err)
		return
	}

	// Set headers (same as before)
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
	// Add Cookie header from cURL if needed, but test without it first.
	// req.Header.Set("Cookie", "jsessionid=s%3A...; __cf_bm=...; _cfuvid=...")

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
		log.Printf("Response Body (first 500 chars): %s", string(body[:min(500, len(body))]))
		return
	}

	var productList ProductListResponse
	err = json.Unmarshal(body, &productList)
	if err != nil {
		log.Printf("Error parsing JSON response: %v", err)
		log.Printf("Response body preview (first 500 chars): %s", string(body[:min(500, len(body))]))
		return
	}

	// --- List Generation ---
	fmt.Printf("\n--- Stock Status Check at %s ---\n", time.Now().Format(time.RFC1123))

	if len(productList.Data) == 0 {
		fmt.Println("No products found in the response for the specified category/filters.")
		fmt.Println("--- End of Check ---")
		return
	}

	var stockList []string // To store status strings for cleaner output
	inStockCount := 0
	outOfStockCount := 0

	// CORRECTED LINE:
	for _, product := range productList.Data { // Use single := here
		var status string
		// Check the 'available' field (or inventory quantity)
		if product.Available == 1 { // Or: product.InventoryQuantity > 0
			status = "IN STOCK"
			inStockCount++
		} else {
			status = "OUT OF STOCK"
			outOfStockCount++
		}
		// Format the output string for each product
		stockList = append(stockList,
			fmt.Sprintf("- %s: %s (Qty: %d, SKU: %s)",
				product.Name,
				status,
				product.InventoryQuantity,
				product.SKU,
			))
	}

	// Print the collected list
	fmt.Println(strings.Join(stockList, "\n"))

	// Print summary
	fmt.Printf("\nSummary: %d products checked. %d IN STOCK, %d OUT OF STOCK.\n",
		len(productList.Data), inStockCount, outOfStockCount)
	fmt.Println("--- End of Check ---")
}

// Helper function to prevent index out of range on short strings
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
