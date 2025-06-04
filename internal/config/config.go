package config

import (
	"errors"
	"flag"
	"log"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type AppConfig struct {
	CheckInterval    time.Duration
	Timezone         *time.Location
	TelegramBotToken string
	TelegramChatId   string
	MonitoredSKUsMap map[string]bool
}

func parseSKUsToBeMonitored(monitoredSKUsRaw string) map[string]bool {
	monitoredSKUsMap := make(map[string]bool)
	for sku := range strings.SplitSeq(monitoredSKUsRaw, ",") {
		trimmedSku := strings.TrimSpace(sku)
		if trimmedSku != "" {
			monitoredSKUsMap[trimmedSku] = true
		}
	}

	log.Printf("Monitoring %d SKU/s", len(monitoredSKUsMap))
	i := 1
	for skuName := range monitoredSKUsMap {
		log.Printf("%d. %s", i, skuName)
		i++
	}
	return monitoredSKUsMap
}

func loadEnvVariables() (string, string, string, error) {
	log.Println("Attempting to load .env file...")
	cwd, _ := os.Getwd()
	log.Printf("Current working directory: %s", cwd)
	if err := godotenv.Load(); err != nil {
		return "", "", "", err
	} else {
		log.Println(".env file loaded successfully (if found).")
	}

	telegramBotToken := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	telegramChatID := strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID"))
	monitoredSKUs := strings.TrimSpace(os.Getenv("MONITORED_SKUS"))

	return telegramBotToken, telegramChatID, monitoredSKUs, nil
}

func ParseConfiguration() (*AppConfig, error) {
	defaultCheckInterval, _ := time.ParseDuration("60m")
	checkIntervalPtr := flag.Duration("check-interval", defaultCheckInterval, "interval at which the app will check for stock")
	monitoredRawSKUs := flag.String("monitored-skus", "", "comma seprated values of SKUs to be monitored")
	timezonePtr := flag.String("timezone", "", "timezone")
	var telegramBotToken, telegramChatID string
	flag.Parse()

	timeLocation, err := time.LoadLocation(*timezonePtr)
	if err != nil {
		log.Println("Failed to parse timezone argument, disabling quiet hours")
	}

	telegramBotToken, telegramChatID, *monitoredRawSKUs, err = loadEnvVariables()
	if err != nil {
		return nil, err
	}

	// Only require telegram bot token for interactive mode
	if telegramBotToken == "" {
		return nil, errors.New("TELEGRAM_BOT_TOKEN is empty. Please set it in your environment or .env file")
	}

	// TELEGRAM_CHAT_ID and monitored-skus are now optional for interactive bot mode
	// They will be provided by individual user subscriptions instead

	log.Printf("Telegram Bot Token Length: %d", len(telegramBotToken))
	if len(telegramBotToken) > 10 {
		log.Printf("Telegram Bot Token Hint: Starts with '%s', ends with '%s'", telegramBotToken[:5], telegramBotToken[len(telegramBotToken)-5:])
	}

	if telegramChatID != "" {
		log.Printf("Telegram Chat ID (fallback): %s", telegramChatID)
	} else {
		log.Println("No fallback Telegram Chat ID set - using interactive mode only")
	}

	var monitoredSKUsMap map[string]bool
	if *monitoredRawSKUs != "" {
		monitoredSKUsMap = parseSKUsToBeMonitored(*monitoredRawSKUs)
		log.Println("Legacy mode: Using predefined SKU list")
	} else {
		monitoredSKUsMap = make(map[string]bool)
		log.Println("Interactive mode: SKUs will be managed through user subscriptions")
	}

	return &AppConfig{
		CheckInterval:    *checkIntervalPtr,
		Timezone:         timeLocation,
		TelegramBotToken: telegramBotToken,
		TelegramChatId:   telegramChatID,
		MonitoredSKUsMap: monitoredSKUsMap,
	}, nil
}
