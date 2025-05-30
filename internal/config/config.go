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

	if *monitoredRawSKUs == "" {
		return nil, errors.New("monitored-skus argument is not set or empty. Please provide a comma-separated list of SKUs")
	}
	if telegramBotToken == "" || telegramChatID == "" {
		return nil, errors.New("TELEGRAM_BOT_TOKEN or TELEGRAM_CHAT_ID is empty. Please set them in your environment or .env file")
	}

	log.Printf("Telegram Bot Token Length: %d", len(telegramBotToken))
	if len(telegramBotToken) > 10 {
		log.Printf("Telegram Bot Token Hint: Starts with '%s', ends with '%s'", telegramBotToken[:5], telegramBotToken[len(telegramBotToken)-5:])
	}
	log.Printf("Telegram Chat ID: %s", telegramChatID)

	return &AppConfig{
		CheckInterval:    *checkIntervalPtr,
		Timezone:         timeLocation,
		TelegramBotToken: telegramBotToken,
		TelegramChatId:   telegramChatID,
		MonitoredSKUsMap: parseSKUsToBeMonitored(*monitoredRawSKUs),
	}, nil
}
