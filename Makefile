# Variables
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
CHROMEDRIVER_VERSION ?= 132.0.6834.83

# Determine OS-specific variables
ifeq ($(GOOS),darwin)
    ifeq ($(GOARCH),arm64)
        CHROMEDRIVER_ARCH=mac-arm64
    else
        CHROMEDRIVER_ARCH=mac-x64
    endif
    CHROMEDRIVER_EXT=.zip
else ifeq ($(GOOS),linux)
    CHROMEDRIVER_ARCH=linux64
    CHROMEDRIVER_EXT=.zip
else ifeq ($(GOOS),windows)
    ifeq ($(GOARCH),386)
        CHROMEDRIVER_ARCH=win32
    else
        CHROMEDRIVER_ARCH=win64
    endif
    CHROMEDRIVER_EXT=.zip
endif

CHROMEDRIVER_URL=https://storage.googleapis.com/chrome-for-testing-public/$(CHROMEDRIVER_VERSION)/$(CHROMEDRIVER_ARCH)/chromedriver-$(CHROMEDRIVER_ARCH)$(CHROMEDRIVER_EXT)

.PHONY: all
all: deps build download-chromedriver

.PHONY: deps
deps:
	@echo "Installing dependencies..."
	go mod download
	go mod tidy

.PHONY: build
build:
	@echo "Building GQL extractor..."
	go build -o bin/gql-extractor

.PHONY: download-chromedriver
download-chromedriver:
	@echo "Downloading ChromeDriver from $(CHROMEDRIVER_URL)..."
	mkdir -p bin/chromedriver
	curl -L "$(CHROMEDRIVER_URL)" -o bin/chromedriver/chromedriver$(CHROMEDRIVER_EXT)
	cd bin/chromedriver && unzip -o chromedriver$(CHROMEDRIVER_EXT)
	chmod +x bin/chromedriver/chromedriver-$(CHROMEDRIVER_ARCH)/chromedriver

.PHONY: clean
clean:
	rm -rf bin/

.PHONY: run
run: all
	@echo "Starting ChromeDriver..."
	./bin/chromedriver/chromedriver-$(CHROMEDRIVER_ARCH)/chromedriver --port=4444 --remote-debugging-port=9222 &
	@echo "Waiting for ChromeDriver to start..."
	sleep 2
	@echo "Running GQL extractor..."
	./bin/gql-extractor --domain="$(DOMAIN)"

.PHONY: stop
stop:
	@echo "Stopping ChromeDriver..."
	pkill chromedriver || true
