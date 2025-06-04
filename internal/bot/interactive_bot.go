package bot

import (
	"amul-notifier/internal/config"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type UserSubscription struct {
	UserID                int64                `json:"user_id"`
	Username              string               `json:"username"`
	MonitoredSKUs         map[string]bool      `json:"monitored_skus"`
	Location              string               `json:"location,omitempty"`               // User's location/region
	StoreCode             string               `json:"store_code,omitempty"`             // API store preference
	NotificationFrequency string               `json:"notification_frequency,omitempty"` // hourly, every_3_hours, every_6_hours, every_12_hours, daily, every_3_days
	LastNotification      map[string]time.Time `json:"last_notification,omitempty"`      // SKU -> last notification time
	LastManualCheck       time.Time            `json:"last_manual_check,omitempty"`      // Rate limiting for manual checks
	CreatedAt             time.Time            `json:"created_at"`
	UpdatedAt             time.Time            `json:"updated_at"`
}

type InteractiveBot struct {
	api           *tgbotapi.BotAPI
	appConfig     *config.AppConfig
	subscriptions map[int64]*UserSubscription
	mutex         sync.RWMutex
	stockBot      *Bot
}

// Available products with their details
var availableProducts = map[string]ProductDetails{
	"DBDCP44_30": {SKU: "DBDCP44_30", Name: "Amul Kool Protein Milkshake | Chocolate, 180 mL | Pack of 30", Category: "Milkshakes"},
	"DBDCP43_30": {SKU: "DBDCP43_30", Name: "Amul Kool Protein Milkshake | Arabica Coffee, 180 mL | Pack of 30", Category: "Milkshakes"},
	"DBDCP42_30": {SKU: "DBDCP42_30", Name: "Amul Kool Protein Milkshake | Kesar, 180 mL | Pack of 30", Category: "Milkshakes"},
	"DBDCP41_30": {SKU: "DBDCP41_30", Name: "Amul High Protein Blueberry Shake, 200 mL | Pack of 30", Category: "Milkshakes"},
	"HPPCP01_02": {SKU: "HPPCP01_02", Name: "Amul High Protein Paneer, 400 g | Pack of 2", Category: "Paneer"},
	"HPPCP01_24": {SKU: "HPPCP01_24", Name: "Amul High Protein Paneer, 400 g | Pack of 24", Category: "Paneer"},
	"WPCCP04_01": {SKU: "WPCCP04_01", Name: "Amul Whey Protein Gift Pack, 32 g | Pack of 10 sachets", Category: "Whey Protein"},
	"WPCCP01_01": {SKU: "WPCCP01_01", Name: "Amul Whey Protein, 32 g | Pack of 30 Sachets", Category: "Whey Protein"},
	"WPCCP02_01": {SKU: "WPCCP02_01", Name: "Amul Whey Protein, 32 g | Pack of 60 Sachets", Category: "Whey Protein"},
	"WPCCP06_01": {SKU: "WPCCP06_01", Name: "Amul Chocolate Whey Protein Gift Pack, 34 g | Pack of 10 sachets", Category: "Whey Protein"},
	"WPCCP03_01": {SKU: "WPCCP03_01", Name: "Amul Chocolate Whey Protein, 34 g | Pack of 30 sachets", Category: "Whey Protein"},
	"WPCCP05_02": {SKU: "WPCCP05_02", Name: "Amul Chocolate Whey Protein, 34 g | Pack of 60 sachets", Category: "Whey Protein"},
	"BTMCP11_30": {SKU: "BTMCP11_30", Name: "Amul High Protein Buttermilk, 200 mL | Pack of 30", Category: "Buttermilk"},
	"LASCP61_30": {SKU: "LASCP61_30", Name: "Amul High Protein Plain Lassi, 200 mL | Pack of 30", Category: "Lassi"},
	"LASCP40_30": {SKU: "LASCP40_30", Name: "Amul High Protein Rose Lassi, 200 mL | Pack of 30", Category: "Lassi"},
	"HPMCP01_08": {SKU: "HPMCP01_08", Name: "Amul High Protein Milk, 250 mL | Pack of 8", Category: "Milk"},
	"HPMCP01_32": {SKU: "HPMCP01_32", Name: "Amul High Protein Milk, 250 mL | Pack of 32", Category: "Milk"},
}

type ProductDetails struct {
	SKU      string
	Name     string
	Category string
}

func NewInteractiveBot(appConfig *config.AppConfig, stockBot *Bot) (*InteractiveBot, error) {
	api, err := tgbotapi.NewBotAPI(appConfig.TelegramBotToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot API: %w", err)
	}

	api.Debug = false
	log.Printf("🤖 Authorized on account %s", api.Self.UserName)

	interactiveBot := &InteractiveBot{
		api:           api,
		appConfig:     appConfig,
		subscriptions: make(map[int64]*UserSubscription),
		stockBot:      stockBot,
	}

	// Load existing subscriptions
	interactiveBot.loadSubscriptions()

	return interactiveBot, nil
}

func (ib *InteractiveBot) Start() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := ib.api.GetUpdatesChan(u)

	log.Println("🚀 Interactive Telegram bot started! Ready to receive messages...")

	for update := range updates {
		if update.Message != nil {
			ib.handleMessage(update.Message)
		} else if update.CallbackQuery != nil {
			ib.handleCallbackQuery(update.CallbackQuery)
		}
	}
}

func (ib *InteractiveBot) handleMessage(message *tgbotapi.Message) {
	userID := message.From.ID
	username := message.From.UserName
	if username == "" {
		username = fmt.Sprintf("%s %s", message.From.FirstName, message.From.LastName)
	}

	log.Printf("📝 Received message from %s (ID: %d): %s", username, userID, message.Text)

	switch message.Text {
	case "/start":
		ib.sendWelcomeMessage(userID)
	case "/help":
		ib.sendHelpMessage(userID)
	case "/menu":
		ib.sendMainMenu(userID)
	case "/mystatus":
		ib.sendUserStatus(userID)
	default:
		// Check if this might be a custom state name
		if ib.isValidStateName(message.Text) {
			ib.setUserLocation(userID, username, strings.ToLower(strings.ReplaceAll(message.Text, " ", "_")))
		} else {
			ib.sendMainMenu(userID)
		}
	}
}

func (ib *InteractiveBot) isValidStateName(input string) bool {
	// Simple validation for state names
	input = strings.TrimSpace(strings.ToLower(input))

	// Must be between 3 and 25 characters, contain only letters, spaces, and underscores
	if len(input) < 3 || len(input) > 25 {
		return false
	}

	for _, char := range input {
		if !((char >= 'a' && char <= 'z') || char == ' ' || char == '_') {
			return false
		}
	}

	return true
}

func (ib *InteractiveBot) sendWelcomeMessage(userID int64) {
	ib.mutex.RLock()
	subscription, hasSubscription := ib.subscriptions[userID]
	ib.mutex.RUnlock()

	// If user already has a location set, show the normal menu
	if hasSubscription && subscription.Location != "" {
		ib.sendMainMenu(userID)
		return
	}

	welcomeText := `
🎉 <b>Welcome to Amul Protein Stock Notifier!</b>

I'll help you stay updated on your favorite Amul protein products. Get instant notifications when items are back in stock!

<b>🚀 What I can do:</b>
• 📋 Browse available products by category
• ✅ Subscribe to stock notifications
• 🔔 Get instant alerts when products are available
• 📊 View your subscription status
• ⚙️ Manage your notification preferences

<b>📍 First, please choose some common states or enter your state name:</b>
`

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🇮🇳 Gujarat", "location_gujarat"),
			tgbotapi.NewInlineKeyboardButtonData("🇮🇳 Maharashtra", "location_maharashtra"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🇮🇳 Punjab", "location_punjab"),
			tgbotapi.NewInlineKeyboardButtonData("🇮🇳 Tamil Nadu", "location_tamil_nadu"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🇮🇳 Karnataka", "location_karnataka"),
			tgbotapi.NewInlineKeyboardButtonData("🇮🇳 West Bengal", "location_west_bengal"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✏️ Enter Custom State", "location_custom"),
		),
	)

	ib.sendMessage(userID, welcomeText, &keyboard)
}

func (ib *InteractiveBot) sendHelpMessage(userID int64) {
	helpText := `
<b>📚 How to use this bot:</b>

<b>🛍️ Browse Products:</b>
Browse available Amul protein products by category and subscribe to notifications.

<b>🔍 Check Stock Now:</b>
Instantly check the current stock status of your subscribed products.

<b>📊 My Status:</b>
View your current subscriptions and manage them.

<b>🔔 Notifications:</b>
You'll receive notifications when your subscribed products come back in stock based on your frequency settings.

<b>⚙️ Notification Settings:</b>
Customize how often you want to receive notifications (30 minutes to 3 days).

<b>💡 Commands:</b>
• /start - Welcome message
• /menu - Main menu
• /mystatus - Your subscription status
• /help - This help message

<b>Need more help?</b> Just send any message and I'll show you the menu! 😊
`

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🏠 Main Menu", "main_menu"),
		),
	)

	ib.sendMessage(userID, helpText, &keyboard)
}

func (ib *InteractiveBot) sendMainMenu(userID int64) {
	menuText := `
<b>🏠 Main Menu</b>

Choose what you'd like to do:
`

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🛍️ Browse Products", "browse_products"),
			tgbotapi.NewInlineKeyboardButtonData("📊 My Status", "my_status"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⚙️ Manage Subscriptions", "manage_subscriptions"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔍 Check Stock Now", "check_stock_now"),
			tgbotapi.NewInlineKeyboardButtonData("🔔 Notification Settings", "notification_settings"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📍 Change State", "change_location"),
			tgbotapi.NewInlineKeyboardButtonData("❓ Help", "help"),
		),
	)

	ib.sendMessage(userID, menuText, &keyboard)
}

func (ib *InteractiveBot) setUserLocation(userID int64, username, location string) {
	// Handle custom state input
	if location == "custom" {
		ib.sendCustomStateInput(userID)
		return
	}

	// Map state codes to readable names (use the state name directly)
	stateMap := map[string]string{
		"gujarat":     "Gujarat",
		"maharashtra": "Maharashtra",
		"punjab":      "Punjab",
		"tamil_nadu":  "Tamil Nadu",
		"karnataka":   "Karnataka",
		"west_bengal": "West Bengal",
	}

	// For API, we'll use the state name in lowercase with underscores
	// This will be used in the setPreferences API call
	stateName, exists := stateMap[location]
	if !exists {
		stateName = strings.Title(strings.ReplaceAll(location, "_", " "))
	}

	// Use the location code as store code for API (convert to lowercase)
	storeCode := strings.ToLower(location)

	ib.mutex.Lock()
	subscription, hasSubscription := ib.subscriptions[userID]
	if !hasSubscription {
		subscription = &UserSubscription{
			UserID:                userID,
			Username:              username,
			MonitoredSKUs:         make(map[string]bool),
			Location:              stateName,
			StoreCode:             storeCode,
			NotificationFrequency: "every_30_minutes", // default: notify every 30 minutes
			LastNotification:      make(map[string]time.Time),
			CreatedAt:             time.Now(),
			UpdatedAt:             time.Now(),
		}
		ib.subscriptions[userID] = subscription
	} else {
		subscription.Location = stateName
		subscription.StoreCode = storeCode
		subscription.UpdatedAt = time.Now()
		if subscription.LastNotification == nil {
			subscription.LastNotification = make(map[string]time.Time)
		}
	}
	ib.mutex.Unlock()

	// Save subscriptions
	ib.saveSubscriptions()

	confirmText := fmt.Sprintf(`
✅ <b>State Set Successfully!</b>

<b>📍 Your State:</b> %s

Great! Now you can browse products and subscribe to notifications. Product availability and delivery information will be personalized based on your state.

<b>Ready to explore?</b> 👇
`, stateName)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🛍️ Browse Products", "browse_products"),
			tgbotapi.NewInlineKeyboardButtonData("📊 My Status", "my_status"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🏠 Main Menu", "main_menu"),
		),
	)

	ib.sendMessage(userID, confirmText, &keyboard)
}

func (ib *InteractiveBot) sendLocationChangeMenu(userID int64) {
	ib.mutex.RLock()
	subscription, hasSubscription := ib.subscriptions[userID]
	ib.mutex.RUnlock()

	currentLocation := "Not set"
	if hasSubscription && subscription.Location != "" {
		currentLocation = subscription.Location
	}

	changeText := fmt.Sprintf(`
📍 <b>Change Your State</b>

<b>Current State:</b> %s

Choose a common state or enter your state name:
`, currentLocation)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🇮🇳 Gujarat", "location_gujarat"),
			tgbotapi.NewInlineKeyboardButtonData("🇮🇳 Maharashtra", "location_maharashtra"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🇮🇳 Punjab", "location_punjab"),
			tgbotapi.NewInlineKeyboardButtonData("🇮🇳 Tamil Nadu", "location_tamil_nadu"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🇮🇳 Karnataka", "location_karnataka"),
			tgbotapi.NewInlineKeyboardButtonData("🇮🇳 West Bengal", "location_west_bengal"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✏️ Enter Custom State", "location_custom"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🏠 Back to Menu", "main_menu"),
		),
	)

	ib.sendMessage(userID, changeText, &keyboard)
}

func (ib *InteractiveBot) sendCustomStateInput(userID int64) {
	customText := `
✏️ <b>Enter Custom State</b>

Please type your state name (e.g., "rajasthan", "kerala", "uttar_pradesh").

The state name will be used for personalized product availability.

<b>Tips:</b>
• Use lowercase letters
• Replace spaces with underscores (e.g., "uttar_pradesh")
• Common examples: rajasthan, kerala, odisha, haryana
`

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🏠 Back to Menu", "main_menu"),
		),
	)

	ib.sendMessage(userID, customText, &keyboard)
}

func (ib *InteractiveBot) checkStockNow(userID int64) {
	ib.mutex.RLock()
	subscription, hasSubscription := ib.subscriptions[userID]
	ib.mutex.RUnlock()

	if !hasSubscription || len(subscription.MonitoredSKUs) == 0 {
		ib.sendMessage(userID, "❌ You don't have any subscriptions to check. Please subscribe to some products first.", nil)
		return
	}

	// Rate limiting: Allow manual checks only every 2 minutes
	const manualCheckCooldown = 2 * time.Minute
	if time.Since(subscription.LastManualCheck) < manualCheckCooldown {
		remaining := manualCheckCooldown - time.Since(subscription.LastManualCheck)
		remainingSeconds := int(remaining.Seconds())
		ib.sendMessage(userID, fmt.Sprintf("⏰ Please wait %d seconds before checking stock again to prevent overloading the system.", remainingSeconds), nil)
		return
	}

	// Update last manual check time
	ib.mutex.Lock()
	ib.subscriptions[userID].LastManualCheck = time.Now()
	ib.subscriptions[userID].UpdatedAt = time.Now()
	ib.mutex.Unlock()

	// Send loading message
	loadingText := `
🔍 <b>Checking Stock Now...</b>

Please wait while I check the current stock status of your subscribed products. This may take a few moments.
`
	ib.sendMessage(userID, loadingText, nil)

	// Trigger stock check for this user's products
	go ib.performManualStockCheck(userID)
}

func (ib *InteractiveBot) performManualStockCheck(userID int64) {
	ib.mutex.RLock()
	subscription, hasSubscription := ib.subscriptions[userID]
	if !hasSubscription {
		ib.mutex.RUnlock()
		return
	}

	userSKUs := make(map[string]bool)
	for sku := range subscription.MonitoredSKUs {
		userSKUs[sku] = true
	}
	ib.mutex.RUnlock()

	if len(userSKUs) == 0 {
		return
	}

	// Use the stock bot to check current stock
	if ib.stockBot != nil {
		// Refresh cookie first
		ib.stockBot.checkCookie()

		// Get current stock status for user's products
		stockResults := ib.stockBot.checkSpecificSKUs(userSKUs)

		// Send results to user
		ib.sendStockCheckResults(userID, stockResults)
	}
}

func (ib *InteractiveBot) sendStockCheckResults(userID int64, results map[string]ProductInfo) {
	if len(results) == 0 {
		ib.sendMessage(userID, "❌ Could not retrieve stock information at this time. Please try again later.", nil)
		return
	}

	resultText := `
📊 <b>Current Stock Status</b>

Here's the current stock status of your subscribed products:

`

	inStockCount := 0
	outOfStockCount := 0

	for sku, product := range results {
		if productDetails, exists := availableProducts[sku]; exists {
			emoji := ib.getCategoryEmoji(productDetails.Category)
			stockEmoji := "❌"
			stockText := "OUT OF STOCK"

			if product.Available == 1 {
				stockEmoji = "✅"
				stockText = fmt.Sprintf("IN STOCK (%d available)", product.InventoryQuantity)
				inStockCount++
			} else {
				outOfStockCount++
			}

			resultText += fmt.Sprintf("• %s %s %s\n  <b>%s</b>\n\n", stockEmoji, emoji, productDetails.Name, stockText)
		}
	}

	resultText += fmt.Sprintf(`
<b>📈 Summary:</b>
✅ In Stock: %d products
❌ Out of Stock: %d products

<i>Last checked: %s</i>
`, inStockCount, outOfStockCount, time.Now().Format("15:04:05 on 02/01/2006"))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Check Again", "check_stock_now"),
			tgbotapi.NewInlineKeyboardButtonData("🏠 Main Menu", "main_menu"),
		),
	)

	ib.sendMessage(userID, resultText, &keyboard)
}

func (ib *InteractiveBot) sendNotificationSettings(userID int64) {
	ib.mutex.RLock()
	subscription, hasSubscription := ib.subscriptions[userID]
	ib.mutex.RUnlock()

	currentFreq := "every_30_minutes"
	if hasSubscription && subscription.NotificationFrequency != "" {
		currentFreq = subscription.NotificationFrequency
	}

	// Map frequency codes to readable names
	freqMap := map[string]string{
		"every_check":      "Every Check",
		"every_30_minutes": "Every 30 Minutes (Default)",
		"hourly":           "Every Hour",
		"every_3_hours":    "Every 3 Hours",
		"every_6_hours":    "Every 6 Hours",
		"every_12_hours":   "Every 12 Hours",
		"daily":            "Daily",
		"every_3_days":     "Every 3 Days",
	}

	currentFreqName, exists := freqMap[currentFreq]
	if !exists {
		currentFreqName = "Every 30 Minutes (Default)"
	}

	settingsText := fmt.Sprintf(`
🔔 <b>Notification Settings</b>

<b>Current Frequency:</b> %s

Choose how often you want to receive stock notifications:
`, currentFreqName)

	var keyboard [][]tgbotapi.InlineKeyboardButton

	for freq, name := range freqMap {
		emoji := "⭕"
		if freq == currentFreq {
			emoji = "✅"
		}
		buttonText := fmt.Sprintf("%s %s", emoji, name)
		button := tgbotapi.NewInlineKeyboardButtonData(buttonText, "freq_"+freq)
		keyboard = append(keyboard, tgbotapi.NewInlineKeyboardRow(button))
	}

	// Add back button
	keyboard = append(keyboard, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🏠 Back to Menu", "main_menu"),
	))

	keyboardMarkup := tgbotapi.NewInlineKeyboardMarkup(keyboard...)
	ib.sendMessage(userID, settingsText, &keyboardMarkup)
}

func (ib *InteractiveBot) setNotificationFrequency(userID int64, frequency string) {
	freqMap := map[string]string{
		"every_check":      "Every Check",
		"every_30_minutes": "Every 30 Minutes (Default)",
		"hourly":           "Every Hour",
		"every_3_hours":    "Every 3 Hours",
		"every_6_hours":    "Every 6 Hours",
		"every_12_hours":   "Every 12 Hours",
		"daily":            "Daily",
		"every_3_days":     "Every 3 Days",
	}

	freqName, exists := freqMap[frequency]
	if !exists {
		ib.sendMessage(userID, "❌ Invalid frequency selected.", nil)
		return
	}

	ib.mutex.Lock()
	subscription, hasSubscription := ib.subscriptions[userID]
	if !hasSubscription {
		subscription = &UserSubscription{
			UserID:                userID,
			Username:              "",
			MonitoredSKUs:         make(map[string]bool),
			NotificationFrequency: frequency,
			LastNotification:      make(map[string]time.Time),
			CreatedAt:             time.Now(),
			UpdatedAt:             time.Now(),
		}
		ib.subscriptions[userID] = subscription
	} else {
		subscription.NotificationFrequency = frequency
		subscription.UpdatedAt = time.Now()
		if subscription.LastNotification == nil {
			subscription.LastNotification = make(map[string]time.Time)
		}
	}
	ib.mutex.Unlock()

	// Save subscriptions
	ib.saveSubscriptions()

	confirmText := fmt.Sprintf(`
✅ <b>Notification Frequency Updated!</b>

<b>🔔 New Setting:</b> %s

Your notifications will now be sent according to your preference.
`, freqName)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔔 Notification Settings", "notification_settings"),
			tgbotapi.NewInlineKeyboardButtonData("🏠 Main Menu", "main_menu"),
		),
	)

	ib.sendMessage(userID, confirmText, &keyboard)
}

func (ib *InteractiveBot) handleCallbackQuery(callback *tgbotapi.CallbackQuery) {
	userID := callback.From.ID
	username := callback.From.UserName
	if username == "" {
		username = fmt.Sprintf("%s %s", callback.From.FirstName, callback.From.LastName)
	}

	log.Printf("🖱️ Callback from %s (ID: %d): %s", username, userID, callback.Data)

	// Answer the callback query to stop the loading animation
	ib.api.Send(tgbotapi.NewCallback(callback.ID, ""))

	switch {
	case callback.Data == "main_menu":
		ib.sendMainMenu(userID)
	case callback.Data == "browse_products":
		ib.sendProductCategories(userID)
	case callback.Data == "my_status":
		ib.sendUserStatus(userID)
	case callback.Data == "manage_subscriptions":
		ib.sendManageSubscriptions(userID)
	case callback.Data == "help":
		ib.sendHelpMessage(userID)
	case callback.Data == "change_location":
		ib.sendLocationChangeMenu(userID)
	case callback.Data == "notification_settings":
		ib.sendNotificationSettings(userID)
	case callback.Data == "check_stock_now":
		ib.checkStockNow(userID)
	case strings.HasPrefix(callback.Data, "freq_"):
		frequency := strings.TrimPrefix(callback.Data, "freq_")
		ib.setNotificationFrequency(userID, frequency)
	case strings.HasPrefix(callback.Data, "location_"):
		location := strings.TrimPrefix(callback.Data, "location_")
		ib.setUserLocation(userID, username, location)
	case strings.HasPrefix(callback.Data, "category_"):
		category := strings.TrimPrefix(callback.Data, "category_")
		ib.sendProductsByCategory(userID, category)
	case strings.HasPrefix(callback.Data, "product_"):
		sku := strings.TrimPrefix(callback.Data, "product_")
		ib.sendProductDetails(userID, sku)
	case strings.HasPrefix(callback.Data, "subscribe_"):
		sku := strings.TrimPrefix(callback.Data, "subscribe_")
		ib.subscribeToProduct(userID, username, sku)
	case strings.HasPrefix(callback.Data, "unsubscribe_"):
		sku := strings.TrimPrefix(callback.Data, "unsubscribe_")
		ib.unsubscribeFromProduct(userID, sku)
	case callback.Data == "back_to_categories":
		ib.sendProductCategories(userID)
	case callback.Data == "back_to_menu":
		ib.sendMainMenu(userID)
	}
}

func (ib *InteractiveBot) sendProductCategories(userID int64) {
	categoriesText := `
<b>🛍️ Product Categories</b>

Choose a category to browse available products:
`

	// Get unique categories
	categories := make(map[string]int)
	for _, product := range availableProducts {
		categories[product.Category]++
	}

	var keyboard [][]tgbotapi.InlineKeyboardButton
	for category, count := range categories {
		emoji := ib.getCategoryEmoji(category)
		buttonText := fmt.Sprintf("%s %s (%d)", emoji, category, count)
		button := tgbotapi.NewInlineKeyboardButtonData(buttonText, "category_"+category)
		keyboard = append(keyboard, tgbotapi.NewInlineKeyboardRow(button))
	}

	// Add back button
	keyboard = append(keyboard, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🏠 Back to Menu", "back_to_menu"),
	))

	keyboardMarkup := tgbotapi.NewInlineKeyboardMarkup(keyboard...)
	ib.sendMessage(userID, categoriesText, &keyboardMarkup)
}

func (ib *InteractiveBot) getCategoryEmoji(category string) string {
	switch category {
	case "Milkshakes":
		return "🥤"
	case "Paneer":
		return "🧀"
	case "Whey Protein":
		return "💪"
	case "Buttermilk":
		return "🥛"
	case "Lassi":
		return "🍯"
	case "Milk":
		return "🥛"
	default:
		return "📦"
	}
}

func (ib *InteractiveBot) sendProductsByCategory(userID int64, category string) {
	emoji := ib.getCategoryEmoji(category)
	categoryText := fmt.Sprintf(`
<b>%s %s Products</b>

Select a product to view details and subscribe:
`, emoji, category)

	var keyboard [][]tgbotapi.InlineKeyboardButton

	// Get products in this category and sort them
	var products []ProductDetails
	for _, product := range availableProducts {
		if product.Category == category {
			products = append(products, product)
		}
	}

	sort.Slice(products, func(i, j int) bool {
		return products[i].Name < products[j].Name
	})

	for _, product := range products {
		// Truncate long names for better display
		displayName := product.Name
		if len(displayName) > 50 {
			displayName = displayName[:47] + "..."
		}

		button := tgbotapi.NewInlineKeyboardButtonData(displayName, "product_"+product.SKU)
		keyboard = append(keyboard, tgbotapi.NewInlineKeyboardRow(button))
	}

	// Add navigation buttons
	keyboard = append(keyboard, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("← Back to Categories", "back_to_categories"),
		tgbotapi.NewInlineKeyboardButtonData("🏠 Main Menu", "back_to_menu"),
	))

	keyboardMarkup := tgbotapi.NewInlineKeyboardMarkup(keyboard...)
	ib.sendMessage(userID, categoryText, &keyboardMarkup)
}

func (ib *InteractiveBot) sendProductDetails(userID int64, sku string) {
	product, exists := availableProducts[sku]
	if !exists {
		ib.sendMessage(userID, "❌ Product not found.", nil)
		return
	}

	// Check if user is subscribed
	ib.mutex.RLock()
	subscription, hasSubscription := ib.subscriptions[userID]
	isSubscribed := hasSubscription && subscription.MonitoredSKUs[sku]
	ib.mutex.RUnlock()

	// Get stock status if available
	stockStatus := "🔍 Checking..."
	if ib.stockBot != nil {
		if stockInfo, exists := ib.stockBot.productDetails[sku]; exists {
			if stockInfo.Available == 1 {
				stockStatus = fmt.Sprintf("✅ In Stock (%d available)", stockInfo.InventoryQuantity)
			} else {
				stockStatus = "❌ Out of Stock"
			}
		}
	}

	emoji := ib.getCategoryEmoji(product.Category)
	productText := fmt.Sprintf(`
<b>%s Product Details</b>

<b>📝 Name:</b> %s

<b>🏷️ SKU:</b> <code>%s</code>

<b>📂 Category:</b> %s %s

<b>📊 Stock Status:</b> %s

<b>🔔 Notification Status:</b> %s
`, emoji, product.Name, product.SKU, emoji, product.Category, stockStatus,
		func() string {
			if isSubscribed {
				return "✅ Subscribed"
			}
			return "❌ Not Subscribed"
		}())

	var keyboard [][]tgbotapi.InlineKeyboardButton

	// Subscribe/Unsubscribe button
	if isSubscribed {
		keyboard = append(keyboard, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔕 Unsubscribe", "unsubscribe_"+sku),
		))
	} else {
		keyboard = append(keyboard, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔔 Subscribe to Notifications", "subscribe_"+sku),
		))
	}

	// Navigation buttons
	keyboard = append(keyboard, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("← Back to "+product.Category, "category_"+product.Category),
		tgbotapi.NewInlineKeyboardButtonData("🏠 Main Menu", "back_to_menu"),
	))

	keyboardMarkup := tgbotapi.NewInlineKeyboardMarkup(keyboard...)
	ib.sendMessage(userID, productText, &keyboardMarkup)
}

func (ib *InteractiveBot) subscribeToProduct(userID int64, username, sku string) {
	product, exists := availableProducts[sku]
	if !exists {
		ib.sendMessage(userID, "❌ Product not found.", nil)
		return
	}

	ib.mutex.Lock()
	subscription, hasSubscription := ib.subscriptions[userID]
	if !hasSubscription {
		subscription = &UserSubscription{
			UserID:        userID,
			Username:      username,
			MonitoredSKUs: make(map[string]bool),
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}
		ib.subscriptions[userID] = subscription
	}

	// Check if user has set their location
	if subscription.Location == "" {
		ib.mutex.Unlock()
		warningText := `
⚠️ <b>Location Required</b>

Please set your location first to get personalized product availability and delivery information.

<b>📍 Select your location:</b>
`

		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("🇮🇳 India (North)", "location_india_north"),
				tgbotapi.NewInlineKeyboardButtonData("🇮🇳 India (South)", "location_india_south"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("🇮🇳 India (East)", "location_india_east"),
				tgbotapi.NewInlineKeyboardButtonData("🇮🇳 India (West)", "location_india_west"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("🇮🇳 India (Central)", "location_india_central"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("🌍 Other Location", "location_other"),
			),
		)

		ib.sendMessage(userID, warningText, &keyboard)
		return
	}

	if subscription.MonitoredSKUs[sku] {
		ib.mutex.Unlock()
		ib.sendMessage(userID, "⚠️ You're already subscribed to this product!", nil)
		return
	}

	subscription.MonitoredSKUs[sku] = true
	subscription.UpdatedAt = time.Now()
	ib.mutex.Unlock()

	// Save subscriptions
	ib.saveSubscriptions()

	successText := fmt.Sprintf(`
✅ <b>Successfully Subscribed!</b>

<b>Product:</b> %s
<b>SKU:</b> <code>%s</code>

🔔 You'll now receive notifications when this product is back in stock!

<b>Total Subscriptions:</b> %d
`, product.Name, sku, len(subscription.MonitoredSKUs))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📊 My Status", "my_status"),
			tgbotapi.NewInlineKeyboardButtonData("🏠 Main Menu", "back_to_menu"),
		),
	)

	ib.sendMessage(userID, successText, &keyboard)
}

func (ib *InteractiveBot) unsubscribeFromProduct(userID int64, sku string) {
	product, exists := availableProducts[sku]
	if !exists {
		ib.sendMessage(userID, "❌ Product not found.", nil)
		return
	}

	ib.mutex.Lock()
	subscription, hasSubscription := ib.subscriptions[userID]
	if !hasSubscription || !subscription.MonitoredSKUs[sku] {
		ib.mutex.Unlock()
		ib.sendMessage(userID, "⚠️ You're not subscribed to this product!", nil)
		return
	}

	delete(subscription.MonitoredSKUs, sku)
	subscription.UpdatedAt = time.Now()
	ib.mutex.Unlock()

	// Save subscriptions
	ib.saveSubscriptions()

	successText := fmt.Sprintf(`
🔕 <b>Successfully Unsubscribed!</b>

<b>Product:</b> %s
<b>SKU:</b> <code>%s</code>

You'll no longer receive notifications for this product.

<b>Remaining Subscriptions:</b> %d
`, product.Name, sku, len(subscription.MonitoredSKUs))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📊 My Status", "my_status"),
			tgbotapi.NewInlineKeyboardButtonData("🏠 Main Menu", "back_to_menu"),
		),
	)

	ib.sendMessage(userID, successText, &keyboard)
}

func (ib *InteractiveBot) sendUserStatus(userID int64) {
	ib.mutex.RLock()
	subscription, hasSubscription := ib.subscriptions[userID]
	ib.mutex.RUnlock()

	if !hasSubscription || len(subscription.MonitoredSKUs) == 0 {
		statusText := `
<b>📊 Your Subscription Status</b>

❌ You don't have any active subscriptions yet.

<b>🚀 Get started:</b>
Browse our products and subscribe to get notifications when they're back in stock!
`

		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("🛍️ Browse Products", "browse_products"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("🏠 Main Menu", "back_to_menu"),
			),
		)

		ib.sendMessage(userID, statusText, &keyboard)
		return
	}

	locationText := subscription.Location
	if locationText == "" {
		locationText = "Not set"
	}

	statusText := fmt.Sprintf(`
<b>📊 Your Subscription Status</b>

<b>👤 User:</b> %s
<b>📍 Location:</b> %s
<b>📅 Member since:</b> %s
<b>🔔 Active subscriptions:</b> %d

<b>📋 Your subscribed products:</b>
`, subscription.Username, locationText, subscription.CreatedAt.Format("Jan 2, 2006"), len(subscription.MonitoredSKUs))

	// List subscribed products
	var subscribedProducts []string
	for sku := range subscription.MonitoredSKUs {
		if product, exists := availableProducts[sku]; exists {
			emoji := ib.getCategoryEmoji(product.Category)
			// Get stock status
			stockStatus := "🔍"
			if ib.stockBot != nil {
				if stockInfo, exists := ib.stockBot.productDetails[sku]; exists {
					if stockInfo.Available == 1 {
						stockStatus = "✅"
					} else {
						stockStatus = "❌"
					}
				}
			}

			displayName := product.Name
			if len(displayName) > 40 {
				displayName = displayName[:37] + "..."
			}
			subscribedProducts = append(subscribedProducts, fmt.Sprintf("%s %s %s", stockStatus, emoji, displayName))
		}
	}

	sort.Strings(subscribedProducts)
	for i, product := range subscribedProducts {
		statusText += fmt.Sprintf("\n%d. %s", i+1, product)
	}

	statusText += "\n\n<b>Legend:</b> ✅ In Stock | ❌ Out of Stock | 🔍 Checking"

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⚙️ Manage Subscriptions", "manage_subscriptions"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🛍️ Browse Products", "browse_products"),
			tgbotapi.NewInlineKeyboardButtonData("🏠 Main Menu", "back_to_menu"),
		),
	)

	ib.sendMessage(userID, statusText, &keyboard)
}

func (ib *InteractiveBot) sendManageSubscriptions(userID int64) {
	ib.mutex.RLock()
	subscription, hasSubscription := ib.subscriptions[userID]
	ib.mutex.RUnlock()

	if !hasSubscription || len(subscription.MonitoredSKUs) == 0 {
		ib.sendMessage(userID, "❌ You don't have any subscriptions to manage.", nil)
		return
	}

	manageText := `
<b>⚙️ Manage Subscriptions</b>

Select a product to unsubscribe:
`

	var keyboard [][]tgbotapi.InlineKeyboardButton

	for sku := range subscription.MonitoredSKUs {
		if product, exists := availableProducts[sku]; exists {
			emoji := ib.getCategoryEmoji(product.Category)
			displayName := product.Name
			if len(displayName) > 45 {
				displayName = displayName[:42] + "..."
			}

			buttonText := fmt.Sprintf("%s %s", emoji, displayName)
			button := tgbotapi.NewInlineKeyboardButtonData(buttonText, "product_"+sku)
			keyboard = append(keyboard, tgbotapi.NewInlineKeyboardRow(button))
		}
	}

	// Add back button
	keyboard = append(keyboard, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("📊 My Status", "my_status"),
		tgbotapi.NewInlineKeyboardButtonData("🏠 Main Menu", "back_to_menu"),
	))

	keyboardMarkup := tgbotapi.NewInlineKeyboardMarkup(keyboard...)
	ib.sendMessage(userID, manageText, &keyboardMarkup)
}

func (ib *InteractiveBot) sendMessage(userID int64, text string, keyboard *tgbotapi.InlineKeyboardMarkup) {
	msg := tgbotapi.NewMessage(userID, text)
	msg.ParseMode = "HTML"
	msg.DisableWebPagePreview = true

	if keyboard != nil {
		msg.ReplyMarkup = keyboard
	}

	if _, err := ib.api.Send(msg); err != nil {
		log.Printf("❌ Error sending message to user %d: %v", userID, err)
	}
}

func (ib *InteractiveBot) saveSubscriptions() {
	ib.mutex.RLock()
	defer ib.mutex.RUnlock()

	data, err := json.MarshalIndent(ib.subscriptions, "", "  ")
	if err != nil {
		log.Printf("❌ Error marshaling subscriptions: %v", err)
		return
	}

	if err := os.WriteFile("subscriptions.json", data, 0644); err != nil {
		log.Printf("❌ Error saving subscriptions: %v", err)
	} else {
		log.Printf("💾 Subscriptions saved successfully")
	}
}

func (ib *InteractiveBot) loadSubscriptions() {
	data, err := os.ReadFile("subscriptions.json")
	if err != nil {
		if os.IsNotExist(err) {
			log.Println("📝 No existing subscriptions file found, starting fresh")
		} else {
			log.Printf("❌ Error reading subscriptions file: %v", err)
		}
		return
	}

	ib.mutex.Lock()
	defer ib.mutex.Unlock()

	if err := json.Unmarshal(data, &ib.subscriptions); err != nil {
		log.Printf("❌ Error unmarshaling subscriptions: %v", err)
		return
	}

	totalSubscriptions := 0
	for _, sub := range ib.subscriptions {
		totalSubscriptions += len(sub.MonitoredSKUs)
	}

	log.Printf("📋 Loaded %d users with %d total subscriptions", len(ib.subscriptions), totalSubscriptions)
}

// SendStockNotificationToSubscribers sends stock notifications to subscribed users
func (ib *InteractiveBot) SendStockNotificationToSubscribers(sku string, productInfo ProductInfo, isInStock bool) {
	ib.mutex.RLock()
	defer ib.mutex.RUnlock()

	product, exists := availableProducts[sku]
	if !exists {
		log.Printf("⚠️ Unknown product SKU for notification: %s", sku)
		return
	}

	emoji := ib.getCategoryEmoji(product.Category)

	var message string
	if isInStock {
		message = fmt.Sprintf(`
🎉 <b>Stock Alert!</b>

%s <b>%s</b> is now <b>IN STOCK!</b>

<b>📊 Details:</b>
• <b>Quantity Available:</b> %d
• <b>SKU:</b> <code>%s</code>
• <b>Category:</b> %s %s

🛒 <b>Order now before it runs out!</b>

<a href="%s%s">🔗 View on Amul Shop</a>
`, emoji, product.Name, productInfo.InventoryQuantity, sku, emoji, product.Category, productBaseURL, productInfo.Alias)
	} else {
		message = fmt.Sprintf(`
😔 <b>Stock Update</b>

%s <b>%s</b> is now <b>OUT OF STOCK</b>

<b>SKU:</b> <code>%s</code>
<b>Category:</b> %s %s

📬 Don't worry! You'll be notified as soon as it's back in stock.
`, emoji, product.Name, sku, emoji, product.Category)
	}

	// Send to all subscribed users
	sentCount := 0
	for userID, subscription := range ib.subscriptions {
		if subscription.MonitoredSKUs[sku] {
			// Check notification frequency first
			if !ib.ShouldSendNotification(userID, sku) {
				log.Printf("🔇 Skipping notification to user %d due to frequency settings", userID)
				continue
			}

			// Check if it's quiet hours for this notification
			if !isQuietHours(ib.appConfig.Timezone) {
				ib.sendMessage(userID, message, nil)
				ib.UpdateLastNotification(userID, sku)
				sentCount++
			} else {
				log.Printf("🔇 Skipping notification to user %d due to quiet hours", userID)
			}
		}
	}

	if sentCount > 0 {
		log.Printf("📤 Sent stock notification for %s to %d subscribers", sku, sentCount)
	}
}

// GetAllSubscribedSKUs returns all SKUs that users are subscribed to
func (ib *InteractiveBot) GetAllSubscribedSKUs() map[string]bool {
	ib.mutex.RLock()
	defer ib.mutex.RUnlock()

	allSKUs := make(map[string]bool)
	for _, subscription := range ib.subscriptions {
		for sku := range subscription.MonitoredSKUs {
			allSKUs[sku] = true
		}
	}

	return allSKUs
}

// GetUserSubscriptions returns all user subscriptions
func (ib *InteractiveBot) GetUserSubscriptions() map[int64]*UserSubscription {
	ib.mutex.RLock()
	defer ib.mutex.RUnlock()

	// Return a copy to avoid race conditions
	result := make(map[int64]*UserSubscription)
	for userID, subscription := range ib.subscriptions {
		result[userID] = subscription
	}

	return result
}

// ShouldSendNotification checks if a user should receive a notification based on their frequency setting
func (ib *InteractiveBot) ShouldSendNotification(userID int64, sku string) bool {
	ib.mutex.RLock()
	subscription, exists := ib.subscriptions[userID]
	ib.mutex.RUnlock()

	if !exists {
		return false
	}

	// Default behavior: send every check
	if subscription.NotificationFrequency == "" || subscription.NotificationFrequency == "every_check" {
		return true
	}

	// Get last notification time for this SKU
	lastNotification, hasLastNotification := subscription.LastNotification[sku]
	if !hasLastNotification {
		return true // First notification for this SKU
	}

	// Calculate time since last notification
	timeSinceLastNotification := time.Since(lastNotification)

	// Check frequency requirements
	switch subscription.NotificationFrequency {
	case "every_30_minutes":
		return timeSinceLastNotification >= 30*time.Minute
	case "hourly":
		return timeSinceLastNotification >= time.Hour
	case "every_3_hours":
		return timeSinceLastNotification >= 3*time.Hour
	case "every_6_hours":
		return timeSinceLastNotification >= 6*time.Hour
	case "every_12_hours":
		return timeSinceLastNotification >= 12*time.Hour
	case "daily":
		return timeSinceLastNotification >= 24*time.Hour
	case "every_3_days":
		return timeSinceLastNotification >= 72*time.Hour
	default:
		return true // Unknown frequency, default to every check
	}
}

// UpdateLastNotification updates the last notification time for a user/SKU combination
func (ib *InteractiveBot) UpdateLastNotification(userID int64, sku string) {
	ib.mutex.Lock()
	defer ib.mutex.Unlock()

	subscription, exists := ib.subscriptions[userID]
	if !exists {
		return
	}

	if subscription.LastNotification == nil {
		subscription.LastNotification = make(map[string]time.Time)
	}

	subscription.LastNotification[sku] = time.Now()
	subscription.UpdatedAt = time.Now()

	// Save updated subscriptions
	go ib.saveSubscriptions()
}
