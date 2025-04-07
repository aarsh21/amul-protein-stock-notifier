# Amul Shop Stock Notifier

## Overview

This Go application monitors the stock availability of specific, user-configured products on the official Amul Shop website (`shop.amul.com`). When a monitored product's stock status changes or meets certain conditions, it sends a notification to a specified Telegram chat, respecting configurable quiet hours.

## Features

* **Periodic Stock Checking:** Checks product availability at a defined interval (default: 10 seconds).
* **Configurable Product List:** Easily define which product SKUs to monitor via an environment variable (`MONITORED_SKUS`).
* **Telegram Notifications:**
  * Sends an alert **every check cycle** if a monitored product is found **in-stock** (outside of quiet hours).
  * Sends an update when a monitored product changes from in-stock to **out-of-stock** (or is assumed out-of-stock if it disappears from the API).
  * Sends an initial notification listing any monitored products that are already **in-stock** when the application starts (respecting quiet hours).
  * Sends a test notification on startup to confirm Telegram configuration is working (respecting quiet hours).
* **Quiet Hours (Do Not Disturb):** Notifications are automatically suppressed during a defined time window (default: 00:00 AM to 07:00 AM IST) to avoid disturbances.
* **Configuration:** Uses a `.env` file or environment variables for easy setup of Telegram credentials and the list of products to monitor.
* **Basic Retry:** Attempts to send Telegram notifications up to 3 times if the initial attempt fails (outside of quiet hours).
* **Logging:** Provides console logs detailing checks, stock status found, notification attempts, and quiet hour suppressions.

## Prerequisites

* **Go:** Version 1.18 or later installed. ([Download Go](https://golang.org/dl/))
* **Telegram Account:** To receive notifications.
* **Telegram Bot:** You need to create a Telegram bot and obtain its **Bot Token**.
  * Talk to the `@BotFather` on Telegram.
  * Use the `/newbot` command and follow the instructions.
  * Copy the **HTTP API token** it gives you.
* **Telegram Chat ID:** You need the ID of the chat (user, group, or channel) where the bot should send notifications.
  * One way to get your user ID is to talk to `@userinfobot` on Telegram.
  * For group chats, add the bot to the group, send a message mentioning the bot, and check the Telegram API response or use helper bots like `@get_id_bot`.
* **Product SKUs:** You need to know the Stock Keeping Units (SKUs) for the Amul products you want to monitor. You might find these by:
  * Looking at the product URL or page source code on `shop.amul.com`.
  * Using browser developer tools to inspect network requests when viewing product pages (look for API calls returning product data).

    Here's a table of some Amul High Protein product SKUs and their corresponding product names (this list might not be exhaustive and availability may vary):

    | SKU         | Product Name                                                  |
    |-------------|---------------------------------------------------------------|
    | DBDCP44_30  | Amul Kool Protein Milkshake \| Chocolate, 180 mL \| Pack of 30      |
    | DBDCP43_30  | Amul Kool Protein Milkshake \| Arabica Coffee, 180 mL \| Pack of 30   |
    | DBDCP42_30  | Amul Kool Protein Milkshake \| Kesar, 180 mL \| Pack of 30        |
    | DBDCP41_30  | Amul High Protein Blueberry Shake, 200 mL \| Pack of 30        |
    | HPPCP01_02  | Amul High Protein Paneer, 400 g \| Pack of 2                     |
    | HPPCP01_24  | Amul High Protein Paneer, 400 g \| Pack of 24                    |
    | WPCCP04_01  | Amul Whey Protein Gift Pack, 32 g \| Pack of 10 sachets          |
    | WPCCP01_01  | Amul Whey Protein, 32 g \| Pack of 30 Sachets               |
    | WPCCP02_01  | Amul Whey Protein, 32 g \| Pack of 60 Sachets               |
    | WPCCP06_01  | Amul Chocolate Whey Protein Gift Pack, 34 g \| Pack of 10 sachets|
    | WPCCP03_01  | Amul Chocolate Whey Protein, 34 g \| Pack of 30 sachets         |
    | WPCCP05_02  | Amul Chocolate Whey Protein, 34 g \| Pack of 60 sachets         |
    | BTMCP11_30  | Amul High Protein Buttermilk, 200 mL \| Pack of 30              |
    | LASCP61_30  | Amul High Protein Plain Lassi, 200 mL \| Pack of 30              |
    | LASCP40_30  | Amul High Protein Rose Lassi, 200 mL \| Pack of 30               |
    | HPMCP01_08  | Amul High Protein Milk, 250 mL \| Pack of 8                   |
    | HPMCP01_32  | Amul High Protein Milk, 250 mL \| Pack of 32                  |

    **Note:** This table provides some examples. You need to find the specific SKUs for the products you wish to monitor on the Amul Shop website.

## Setup

1. **Clone the Repository (If applicable):**

    ```bash
    git clone <repository_url>
    cd <repository_directory_name>
    ```

    *(Replace `<repository_url>` and `<repository_directory_name>`)*
    If you just have the `main.go` file, simply navigate to that directory in your terminal.

2. **Configure Credentials and Products:**
    Create a file named `.env` in the project's root directory (where `main.go` is). Add your Telegram Bot Token, Chat ID, and the list of SKUs to monitor:

    ```dotenv
    # .env file

    # Required: Your Telegram Bot Token from BotFather
    TELEGRAM_BOT_TOKEN=YOUR_TELEGRAM_BOT_TOKEN_HERE

    # Required: The ID of the chat where notifications should be sent
    TELEGRAM_CHAT_ID=YOUR_TELEGRAM_CHAT_ID_HERE

    # Required: Comma-separated list of Amul product SKUs to monitor
    MONITORED_SKUS=LASCP61_30,LASCP40_30,DBDCP42_30,DBDCP41_30
    ```

    * Replace `YOUR_TELEGRAM_BOT_TOKEN_HERE` with the token you got from BotFather.
    * Replace `YOUR_TELEGRAM_CHAT_ID_HERE` with the target chat's ID.
    * Replace the example SKUs in `MONITORED_SKUS` with the actual SKUs you want to track, separated by commas (no spaces around commas).

    Alternatively, you can set `TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID`, and `MONITORED_SKUS` as environment variables directly in your system before running the application.

3. **Install Dependencies (Usually Automatic):**
    Go typically handles dependencies automatically during the build. If you encounter issues, run:

    ```bash
    go mod tidy
    ```

4. **Build the Application:**
    Open your terminal in the project directory and run:

    ```bash
    go build -o amul-stock-notifier .
    ```

    *(This creates an executable file named `amul-stock-notifier` in the current directory, or `amul-stock-notifier.exe` on Windows)*

## Running

Ensure the `.env` file is present in the same directory as the executable, or that the required environment variables are set in your session. Then, execute the compiled binary from your terminal:

```bash
./amul-stock-notifier
