# Simple Makefile for a Go project

ARGS ?=

# Build the application
all: build test

build:
	@echo "Building Amul Stock Notifier Bot..."
	@go build -o amul-bot cmd/main.go

# Run the application
run:
	@go run cmd/main.go $(ARGS)

# Test the application
test:
	@echo "Testing..."
	@go test ./... -v

# Clean the binaries
clean:
	@echo "Cleaning..."
	@rm -f amul-bot

# Live Reload
watch:
	@if command -v air > /dev/null; then \
            air; \
            echo "Watching...";\
        else \
            read -p "Go's 'air' is not installed on your machine. Do you want to install it? [Y/n] " choice; \
            if [ "$$choice" != "n" ] && [ "$$choice" != "N" ]; then \
                go install github.com/air-verse/air@latest; \
                air; \
                echo "Watching...";\
            else \
                echo "You chose not to install air. Exiting..."; \
                exit 1; \
            fi; \
        fi

.PHONY: all build run test clean watch
