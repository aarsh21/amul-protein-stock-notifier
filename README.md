# Amul Shop Stock Notifier

## Overview

This Go application monitors the stock availability of specific products (currently Lassi and Milkshakes) on the official Amul Shop website (`shop.amul.com`). When a monitored product comes back in stock or goes out of stock, it sends a notification to a specified Telegram chat.

## Features

* **Periodic Stock Checking:** Checks product availability at a defined interval (default: 5 minutes).
* **Telegram Notifications:**
  * Sends an alert when a monitored product changes from out-of-stock to **in-stock**.
  * Sends an update when a monitored product changes from in-stock to **out-of-stock**.
  * Sends an initial notification listing any monitored products that are already **in-stock** when the application starts.
  * Sends a test notification on startup to confirm Telegram configuration is working.
* **Daily Summary:** Sends a summary of the stock status for all monitored products around 9:00 AM daily.
* **Configuration:** Uses a `.env` file or environment variables for easy setup of Telegram credentials.
* **Basic Retry:** Attempts to send Telegram notifications up to 3 times if the initial attempt fails.
* **Logging:** Provides console logs detailing checks, stock status found, and notification attempts.

## Prerequisites

* **Go:** Version 1.18 or later installed. ([Download Go](https://golang.org/dl/))
* **Telegram Account:** To receive notifications.
* **Telegram Bot:** You need to create a Telegram bot and obtain its **Bot Token**.
  * Talk to the `@BotFather` on Telegram.
  * Use the `/newbot` command and follow the instructions.
  * Copy the **HTTP API token** it gives you.
* **Telegram Chat ID:** You need the ID of the chat (user, group, or channel) where the bot should send notifications.
  * One way to get your user ID is to talk to `@userinfobot` on Telegram.
  * For group chats, add the bot to the group and potentially use a bot like `@get_id_bot` or check bot API responses.

## Setup

1. **Clone the Repository:**

    ```bash
    git clone <repository_url>
    cd <repository_directory_name>
    ```

    *(Replace `<repository_url>` and `<repository_directory_name>`)*

2. **Configure Credentials:**
    Create a file named `.env` in the project's root directory. Add your Telegram Bot Token and Chat ID to this file:

    ```dotenv
    # .env file
    TELEGRAM_BOT_TOKEN=YOUR_TELEGRAM_BOT_TOKEN_HERE
    TELEGRAM_CHAT_ID=YOUR_TELEGRAM_CHAT_ID_HERE
    ```

    * Replace `YOUR_TELEGRAM_BOT_TOKEN_HERE` with the token you got from BotFather.
    * Replace `YOUR_TELEGRAM_CHAT_ID_HERE` with the target chat's ID.

    Alternatively, you can set these as environment variables directly in your system.

3. **Install Dependencies:**
    Go should handle this automatically when building, but you can run `go mod tidy` if needed.

4. **Build the Application:**

    ```bash
    go build -o amul-stock-notifier .
    ```

    *(This creates an executable file named `amul-stock-notifier` or `amul-stock-notifier.exe` on Windows)*

## Running

Execute the compiled binary from your terminal:

```bash
./amul-stock-notifier
