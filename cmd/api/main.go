package main

import (
	"log"
	"time"

	"amul-notifier/internal/bot"
	"amul-notifier/internal/config"
)

func main() {
	appConfig , err := config.ParseConfiguration()
	if err != nil {
		log.Fatalf("Failed to parse configuration with error[%s]", err.Error())
	}

	log.Println("Starting Amul product stock notifier...")
	amulBot, err := bot.InitBot(appConfig)
	if err != nil {
		log.Fatalf("Failed to initialize bot with error[%s]", err.Error())
	}

	bot.StartupTestNotification(appConfig)
	bot.CheckTargetStock(amulBot)
	bot.SendInitialStockNotifications(amulBot)
	
	bot.SetBotFirstRun(amulBot)
	log.Printf("Initial setup complete. Regular checks starting with check-interval[%v]", appConfig.CheckInterval)
	ticker := time.NewTicker(appConfig.CheckInterval)
	defer ticker.Stop()

	for range ticker.C {
		bot.CheckTargetStock(amulBot)
	}
}
