package main

import (
	"amul-notifier/internal/bot"
	"amul-notifier/internal/config"
	"log"
	"time"
)

func main() {
	appConfig, err := config.ParseConfiguration()
	if err != nil {
		log.Fatalf("Failed to parse configuration with error[%s]", err.Error())
	}

	log.Println("🚀 Starting Amul Stock Notifier Bot...")

	// Initialize the stock checking bot
	stockBot, err := bot.InitBot(appConfig)
	if err != nil {
		log.Fatalf("Failed to initialize stock bot with error[%s]", err.Error())
	}

	// Initialize the interactive Telegram bot
	interactiveBot, err := bot.NewInteractiveBot(appConfig, stockBot)
	if err != nil {
		log.Fatalf("Failed to initialize interactive bot with error[%s]", err.Error())
	}

	// Link the bots together
	stockBot.SetInteractiveBot(interactiveBot)

	// Start the interactive bot in a goroutine
	go interactiveBot.Start()

	// Do initial stock check for monitored products (if any configured)
	bot.CheckTargetStock(stockBot)

	bot.SetBotFirstRun(stockBot)
	log.Printf("🎯 Initial setup complete. Regular checks starting with check-interval[%v]", appConfig.CheckInterval)
	log.Printf("📱 Interactive bot is ready! Users can now message your bot to subscribe to notifications.")

	ticker := time.NewTicker(appConfig.CheckInterval)
	defer ticker.Stop()

	for range ticker.C {
		bot.CheckTargetStock(stockBot)
	}
}
