#!/bin/bash

# 🤖 Amul Stock Notifier Bot - Quick Start Script
# This script helps you set up and run the interactive Telegram bot

echo "🚀 Amul Stock Notifier Bot - Quick Start"
echo "======================================="

# Check if .env file exists
if [ ! -f ".env" ]; then
  echo "❌ .env file not found!"
  echo "📝 Please create a .env file with your Telegram bot token."
  echo "💡 You can copy env.example to .env and fill in your values:"
  echo "   cp env.example .env"
  echo "   nano .env"
  echo ""
  echo "🤖 To create a Telegram bot:"
  echo "   1. Message @BotFather on Telegram"
  echo "   2. Use /newbot command and follow instructions"
  echo "   3. Copy the token to your .env file"
  exit 1
fi

# Check if bot token is set
if ! grep -q "TELEGRAM_BOT_TOKEN=.*[^[:space:]]" .env; then
  echo "❌ TELEGRAM_BOT_TOKEN not set in .env file!"
  echo "📝 Please add your bot token to the .env file."
  exit 1
fi

echo "✅ Configuration looks good!"

# Build the bot if needed
if [ ! -f "amul-bot" ]; then
  echo "🔨 Building bot..."
  make build
  if [ $? -ne 0 ]; then
    echo "❌ Build failed!"
    exit 1
  fi
fi

echo "🎯 Starting the bot..."
echo "📱 Users can now message your bot to subscribe to notifications!"
echo "⏰ Using default settings: 60min check interval"
echo "🔇 Quiet hours: 00:00-07:00 (disable with --timezone='')"
echo ""
echo "🛑 Press Ctrl+C to stop the bot"
echo "======================================="

# Run with default settings, you can customize these
./amul-bot --timezone="Asia/Kolkata" --check-interval="15m"

