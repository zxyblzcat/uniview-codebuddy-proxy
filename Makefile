APP_NAME := codebuddy-proxy
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X codebuddy-proxy/internal/version.Version=$(VERSION) -X codebuddy-proxy/internal/version.Commit=$(COMMIT) -X codebuddy-proxy/internal/version.Date=$(DATE)

.PHONY: build build-all clean run

build:
	go build -ldflags "$(LDFLAGS)" -o $(APP_NAME) ./cmd/proxy

build-all: build-darwin-arm64 build-darwin-amd64 build-linux-amd64 build-linux-arm64 build-windows-amd64 build-windows-arm64

build-darwin-arm64:
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(APP_NAME)_$(VERSION)_darwin_arm64 ./cmd/proxy

build-darwin-amd64:
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(APP_NAME)_$(VERSION)_darwin_amd64 ./cmd/proxy

build-linux-amd64:
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(APP_NAME)_$(VERSION)_linux_amd64 ./cmd/proxy

build-linux-arm64:
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(APP_NAME)_$(VERSION)_linux_arm64 ./cmd/proxy

build-windows-amd64:
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(APP_NAME)_$(VERSION)_windows_amd64.exe ./cmd/proxy

build-windows-arm64:
	GOOS=windows GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(APP_NAME)_$(VERSION)_windows_arm64.exe ./cmd/proxy

clean:
	rm -f $(APP_NAME) $(APP_NAME)_$(VERSION)_darwin_arm64 $(APP_NAME)_$(VERSION)_darwin_amd64 $(APP_NAME)_$(VERSION)_linux_amd64 $(APP_NAME)_$(VERSION)_linux_arm64 $(APP_NAME)_$(VERSION)_windows_amd64.exe $(APP_NAME)_$(VERSION)_windows_arm64.exe

run:
	go run ./cmd/proxy
