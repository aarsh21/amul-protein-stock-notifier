# 🤖 Amul Stock Notifier Bot

## 🌟 Overview

This is a beautiful, interactive Telegram bot that monitors Amul protein product stock and sends personalized notifications! Users can easily browse products, subscribe to notifications, and manage their preferences through an intuitive UI with buttons and menus.

## ✨ Key Features

### 🎨 Amazing Telegram UI
- **📱 Interactive Inline Keyboards**: Beautiful button-based navigation
- **🎯 Category-based Browsing**: Organized product categories with emojis
- **📊 Real-time Status Display**: Live stock status indicators
- **⚡ Instant Feedback**: Immediate responses to user actions

### 🔔 Smart Notification System
- **👥 Multi-user Support**: Each user manages their own subscriptions
- **🎯 Personalized Alerts**: Only get notified for products you care about
- **📍 Location-based Experience**: Set your region for personalized product availability
- **🔇 Quiet Hours Respect**: Respects configured quiet hours
- **📬 Rich Notifications**: Detailed stock alerts with product links

### 🛍️ Product Management
- **📋 17 Available Products**: All Amul protein products categorized
- **🧀 Categories**: Milkshakes, Paneer, Whey Protein, Buttermilk, Lassi, Milk
- **✅ Easy Subscription**: One-click subscribe/unsubscribe
- **📈 Stock Tracking**: Real-time inventory monitoring

### 💾 Data Persistence
- **🗄️ JSON-based Storage**: User subscriptions saved locally
- **🔄 Auto-save**: Subscriptions automatically saved on changes
- **📱 Cross-session Memory**: Bot remembers user preferences

## 🚀 Getting Started

### Prerequisites

1. **Telegram Bot Token**: Create a bot with [@BotFather](https://t.me/BotFather)
2. **Go 1.18+**: [Download Go](https://golang.org/dl/)
3. **Environment Setup**: Configure your `.env` file

### 🔧 Installation

1. **Clone and Setup**:
   ```bash
   git clone <your-repo>
   cd amul-protein-stock-notifier
   ```

2. **Install Dependencies**:
   ```bash
   go mod download
   ```

3. **Configure Environment**:
   Create a `.env` file:
   ```env
   # Required: Your Telegram Bot Token from BotFather
   TELEGRAM_BOT_TOKEN=YOUR_BOT_TOKEN_HERE
   
   # Optional: Fallback chat ID for system notifications (legacy mode only)
   # Leave empty for pure interactive mode
   # TELEGRAM_CHAT_ID=YOUR_CHAT_ID
   ```

4. **Build the Bot**:
   ```bash
   make build
   ```

5. **Run the Bot**:
   ```bash
   ./amul-bot --timezone="Asia/Kolkata" --check-interval="30m"
   ```

   Or use the Makefile:
   ```bash
   make run ARGS="--timezone=Asia/Kolkata --check-interval=30m"
   ```

## 📱 Bot Commands & Features

### 🎮 Basic Commands
- `/start` - Welcome message and main menu
- `/help` - Comprehensive help information
- `/menu` - Access the main menu anytime
- `/mystatus` - Quick access to your subscription status

### 🧭 Navigation Flow

```
🏠 Main Menu
├── 🛍️ Browse Products
│   ├── 🥤 Milkshakes (4 products)
│   ├── 🧀 Paneer (2 products)
│   ├── 💪 Whey Protein (6 products)
│   ├── 🥛 Buttermilk (1 product)
│   ├── 🍯 Lassi (2 products)
│   └── 🥛 Milk (2 products)
├── 📊 My Status
│   └── View all subscriptions with stock status and location
├── ⚙️ Manage Subscriptions
│   └── Easy unsubscribe interface
├── 📍 Change Location
│   └── Update your region for personalized availability
└── ❓ Help
    └── Detailed usage instructions
```

### 🎯 Product Interaction

When you select a product, you'll see:
- **📝 Product Name**: Full product description
- **🏷️ SKU**: Product identification code
- **📂 Category**: Product category with emoji
- **📊 Stock Status**: Real-time availability
- **🔔 Subscription Status**: Whether you're subscribed
- **🔄 Quick Actions**: Subscribe/Unsubscribe buttons

## 🔔 Notification Examples

### ✅ Stock Available Notification
```
🎉 Stock Alert!

💪 Amul Whey Protein, 32 g | Pack of 30 Sachets is now IN STOCK!

📊 Details:
• Quantity Available: 15
• SKU: WPCCP01_01
• Category: 💪 Whey Protein

🛒 Order now before it runs out!

🔗 View on Amul Shop
```

### ❌ Out of Stock Notification
```
😔 Stock Update

🥤 Amul Kool Protein Milkshake | Chocolate, 180 mL | Pack of 30 is now OUT OF STOCK

SKU: DBDCP44_30
Category: 🥤 Milkshakes

📬 Don't worry! You'll be notified as soon as it's back in stock.
```

## 🏗️ Architecture

### 📁 File Structure
```
cmd/
└── main.go                 # Main application entry point

internal/
├── bot/
│   ├── bot.go              # Stock checking & notification logic
│   └── interactive_bot.go  # Interactive UI & user management
└── config/
    └── config.go           # Configuration management
```

### 🔄 Component Interaction
```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│  Interactive    │    │   Stock Bot      │    │   Amul API      │
│     Bot         │◄──►│                  │◄──►│                 │
│                 │    │                  │    │                 │
│ • User Management   │ • Stock Checking │    │ • Product Data  │
│ • UI/UX         │    │ • Cookie Mgmt    │    │ • Availability  │
│ • Notifications │    │ • API Requests   │    │                 │
└─────────────────┘    └──────────────────┘    └─────────────────┘
```

## ⚙️ Configuration Options

### 🚩 Command Line Flags
- `--check-interval`: How often to check stock (default: 60m)
- `--timezone`: Timezone for quiet hours (e.g., "Asia/Kolkata")
- `--monitored-skus`: Legacy fallback SKU list (not needed for interactive mode)

### 🌙 Quiet Hours
- **Default**: 00:00 - 07:00 in specified timezone
- **Behavior**: Notifications are suppressed during quiet hours
- **Override**: Set timezone to disable quiet hours

## 📊 Available Products

| Category | Count | Examples |
|----------|-------|----------|
| 🥤 Milkshakes | 4 | Chocolate, Coffee, Kesar, Blueberry |
| 🧀 Paneer | 2 | High Protein Paneer (2-pack, 24-pack) |
| 💪 Whey Protein | 6 | Regular & Chocolate (various pack sizes) |
| 🥛 Buttermilk | 1 | High Protein Buttermilk |
| 🍯 Lassi | 2 | Plain & Rose Lassi |
| 🥛 Milk | 2 | High Protein Milk (8-pack, 32-pack) |

## 🔧 Development

### 🛠️ Build Commands
```bash
# Build the application
make build

# Run the application
make run

# Run with live reload
make watch
```

### 🧪 Testing
```bash
# Run tests
make test

# Clean builds
make clean
```

## 🤝 Usage Tips

### 👑 Best Practices
1. **Start with `/start`**: Get familiar with the interface
2. **Browse by Category**: Organized browsing is easier
3. **Check Status Regularly**: Use "My Status" to see stock updates
4. **Manage Subscriptions**: Remove products you're no longer interested in

### 🎯 Pro Tips
- **Quick Access**: Send any message to get the main menu
- **Stock Indicators**: ✅ = In Stock, ❌ = Out of Stock, 🔍 = Checking
- **Instant Updates**: Changes are saved immediately
- **Rich Text**: Use copy button on SKUs for easy reference

## 🆚 What's New

This bot provides a completely interactive experience compared to traditional CLI-based stock checkers:

| Feature | Traditional CLI | This Bot |
|---------|-------------|-----------------|
| User Interface | Command line flags | Beautiful Telegram UI |
| User Management | Single admin | Multi-user support |
| Product Selection | Manual SKU entry | Visual browsing |
| Subscription Management | Global config | Individual preferences |
| Notifications | Fixed recipient | Per-user targeting |
| Ease of Use | Technical | User-friendly |

## 🚨 Troubleshooting

### Common Issues

1. **Bot Not Responding**:
   - Check your `TELEGRAM_BOT_TOKEN`
   - Ensure bot is started with `/start`

2. **No Notifications**:
   - Verify you're subscribed to products
   - Check if it's during quiet hours

3. **Products Not Loading**:
   - Check internet connection
   - Verify Amul API accessibility

### 📝 Logs
The bot provides detailed logging:
- 🤖 Bot authorization
- 📝 User interactions
- 🖱️ Button callbacks  
- 📤 Notification sending
- 💾 Data persistence

## 🔮 Future Enhancements

- 📊 **Analytics Dashboard**: Usage statistics
- 🔍 **Search Functionality**: Search products by name
- 🏷️ **Price Tracking**: Price change notifications
- 📱 **Mobile App**: Native mobile experience
- 🌐 **Web Interface**: Browser-based management
- 🤖 **AI Integration**: Smart product recommendations

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

---

**🎉 Enjoy your enhanced Amul protein stock notifications with the beautiful interactive interface!** 