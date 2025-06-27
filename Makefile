# Variables
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

# Chrome version detection
ifeq ($(GOOS),darwin)
    CHROME_VERSION := $(shell "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+' || echo "")
    ifeq ($(GOARCH),arm64)
        CHROMEDRIVER_ARCH=mac-arm64
    else
        CHROMEDRIVER_ARCH=mac-x64
    endif
    CHROMEDRIVER_EXT=.zip
else ifeq ($(GOOS),linux)
    CHROME_VERSION := $(shell google-chrome --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+' || chromium --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+' || echo "")
    CHROMEDRIVER_ARCH=linux64
    CHROMEDRIVER_EXT=.zip
else ifeq ($(GOOS),windows)
    CHROME_VERSION := $(shell reg query "HKEY_CURRENT_USER\Software\Google\Chrome\BLBeacon" /v version 2>nul | grep -oE '[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+' || echo "")
    ifeq ($(GOARCH),386)
        CHROMEDRIVER_ARCH=win32
    else
        CHROMEDRIVER_ARCH=win64
    endif
    CHROMEDRIVER_EXT=.zip
endif

# If Chrome version not detected, use a default
ifeq ($(CHROME_VERSION),)
    CHROME_VERSION := 138.0.7204.49
endif

# Extract major version for ChromeDriver
CHROME_MAJOR := $(shell echo $(CHROME_VERSION) | cut -d. -f1)

# ChromeDriver URL - uses the Chrome for Testing API
CHROMEDRIVER_URL := https://storage.googleapis.com/chrome-for-testing-public/$(CHROME_VERSION)/$(CHROMEDRIVER_ARCH)/chromedriver-$(CHROMEDRIVER_ARCH)$(CHROMEDRIVER_EXT)
CHROMEDRIVER_API_URL := https://googlechromelabs.github.io/chrome-for-testing/known-good-versions-with-downloads.json

# Ports
SELENIUM_PORT ?= 4444
DEBUG_PORT ?= 9222

.PHONY: all
all: deps build ensure-chromedriver

.PHONY: deps
deps:
	@echo "Installing dependencies..."
	go mod download
	go mod tidy

.PHONY: build
build:
	@echo "Building GQL extractor..."
	go build -o bin/gql-extractor

.PHONY: check-chrome
check-chrome:
	@echo "Detected Chrome version: $(CHROME_VERSION)"
	@echo "Chrome major version: $(CHROME_MAJOR)"

.PHONY: get-chromedriver-version
get-chromedriver-version:
	@echo "Finding matching ChromeDriver version for Chrome $(CHROME_VERSION)..."
	@curl -s $(CHROMEDRIVER_API_URL) | \
		jq -r '.versions[] | select(.version | startswith("$(CHROME_MAJOR).")) | .downloads.chromedriver[] | select(.platform == "$(CHROMEDRIVER_ARCH)") | .url' | \
		head -1 || echo $(CHROMEDRIVER_URL)

.PHONY: ensure-chromedriver
ensure-chromedriver:
	@if [ ! -f bin/chromedriver/chromedriver-$(CHROMEDRIVER_ARCH)/chromedriver ]; then \
		echo "ChromeDriver not found, downloading..."; \
		$(MAKE) download-chromedriver; \
	else \
		echo "ChromeDriver already exists at bin/chromedriver/chromedriver-$(CHROMEDRIVER_ARCH)/chromedriver"; \
	fi

.PHONY: download-chromedriver
download-chromedriver:
	@echo "Downloading ChromeDriver for Chrome $(CHROME_VERSION)..."
	@mkdir -p bin/chromedriver
	@DRIVER_URL=$$(curl -s $(CHROMEDRIVER_API_URL) | \
		jq -r '.versions[] | select(.version | startswith("$(CHROME_MAJOR).")) | .downloads.chromedriver[] | select(.platform == "$(CHROMEDRIVER_ARCH)") | .url' | \
		head -1); \
	if [ -z "$$DRIVER_URL" ]; then \
		echo "Could not find exact match, using closest version..."; \
		DRIVER_URL=$(CHROMEDRIVER_URL); \
	fi; \
	echo "Downloading from: $$DRIVER_URL"; \
	curl -L "$$DRIVER_URL" -o bin/chromedriver/chromedriver$(CHROMEDRIVER_EXT)
	cd bin/chromedriver && unzip -o chromedriver$(CHROMEDRIVER_EXT)
	chmod +x bin/chromedriver/chromedriver-$(CHROMEDRIVER_ARCH)/chromedriver
	@echo "ChromeDriver downloaded successfully"

.PHONY: clean
clean:
	@echo "Cleaning up..."
	rm -rf bin/
	$(MAKE) stop

.PHONY: check-ports
check-ports:
	@echo "Checking if ports are available..."
	@if lsof -i:$(SELENIUM_PORT) >/dev/null 2>&1; then \
		echo "Port $(SELENIUM_PORT) is already in use. Killing process..."; \
		lsof -ti:$(SELENIUM_PORT) | xargs kill -9 2>/dev/null || true; \
		sleep 1; \
	fi
	@if lsof -i:$(DEBUG_PORT) >/dev/null 2>&1; then \
		echo "Port $(DEBUG_PORT) is already in use. Killing process..."; \
		lsof -ti:$(DEBUG_PORT) | xargs kill -9 2>/dev/null || true; \
		sleep 1; \
	fi

.PHONY: run
run: all check-ports
	@if [ -z "$(DOMAIN)" ]; then \
		echo "Error: DOMAIN is required. Usage: make run DOMAIN=https://example.com"; \
		exit 1; \
	fi
	@echo "Starting ChromeDriver on port $(SELENIUM_PORT)..."
	@./bin/chromedriver/chromedriver-$(CHROMEDRIVER_ARCH)/chromedriver --port=$(SELENIUM_PORT) --log-level=WARNING > chromedriver.log 2>&1 & \
		CHROMEDRIVER_PID=$$!; \
		echo "ChromeDriver PID: $$CHROMEDRIVER_PID"; \
		echo "Waiting for ChromeDriver to start..."; \
		for i in 1 2 3 4 5; do \
			if curl -s http://localhost:$(SELENIUM_PORT)/status >/dev/null; then \
				echo "ChromeDriver is ready"; \
				break; \
			fi; \
			if [ $$i -eq 5 ]; then \
				echo "ChromeDriver failed to start. Check chromedriver.log"; \
				cat chromedriver.log; \
				exit 1; \
			fi; \
			sleep 1; \
		done; \
		echo "Running GQL extractor..."; \
		./bin/gql-extractor --domain="$(DOMAIN)" || (kill $$CHROMEDRIVER_PID 2>/dev/null; exit 1); \
		kill $$CHROMEDRIVER_PID 2>/dev/null || true

.PHONY: run-detached
run-detached: all check-ports
	@if [ -z "$(DOMAIN)" ]; then \
		echo "Error: DOMAIN is required. Usage: make run-detached DOMAIN=https://example.com"; \
		exit 1; \
	fi
	@echo "Starting ChromeDriver in detached mode..."
	@nohup ./bin/chromedriver/chromedriver-$(CHROMEDRIVER_ARCH)/chromedriver --port=$(SELENIUM_PORT) --log-level=WARNING > chromedriver.log 2>&1 & \
		echo $$! > chromedriver.pid
	@echo "Waiting for ChromeDriver to start..."
	@for i in 1 2 3 4 5; do \
		if curl -s http://localhost:$(SELENIUM_PORT)/status >/dev/null; then \
			echo "ChromeDriver is ready"; \
			break; \
		fi; \
		sleep 1; \
	done
	@echo "Running GQL extractor..."
	./bin/gql-extractor --domain="$(DOMAIN)"

.PHONY: stop
stop:
	@echo "Stopping ChromeDriver..."
	@if [ -f chromedriver.pid ]; then \
		kill $$(cat chromedriver.pid) 2>/dev/null || true; \
		rm -f chromedriver.pid; \
	fi
	@pkill -f "chromedriver.*--port=$(SELENIUM_PORT)" 2>/dev/null || true
	@lsof -ti:$(SELENIUM_PORT) | xargs kill -9 2>/dev/null || true
	@lsof -ti:$(DEBUG_PORT) | xargs kill -9 2>/dev/null || true
	@echo "ChromeDriver stopped"

.PHONY: logs
logs:
	@if [ -f chromedriver.log ]; then \
		tail -f chromedriver.log; \
	else \
		echo "No log file found"; \
	fi

.PHONY: help
help:
	@echo "GQL Extractor Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make run DOMAIN=https://example.com  - Build and run the extractor"
	@echo "  make run-detached DOMAIN=...         - Run ChromeDriver in background"
	@echo "  make stop                            - Stop ChromeDriver"
	@echo "  make clean                           - Clean build artifacts"
	@echo "  make check-chrome                    - Check Chrome version"
	@echo "  make check-ports                     - Check if ports are available"
	@echo "  make logs                            - Tail ChromeDriver logs"
	@echo ""
	@echo "Options:"
	@echo "  SELENIUM_PORT=4444                   - Change Selenium port (default: 4444)"
	@echo "  DEBUG_PORT=9222                      - Change debug port (default: 9222)"