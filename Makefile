APP_NAME := codebuddy-proxy

.PHONY: build build-all clean run

build:
	go build -o $(APP_NAME) ./cmd/proxy

build-all: build-mac-arm build-mac-intel build-windows-amd64

build-mac-arm:
	GOOS=darwin GOARCH=arm64 go build -o $(APP_NAME)-mac-arm64 ./cmd/proxy

build-mac-intel:
	GOOS=darwin GOARCH=amd64 go build -o $(APP_NAME)-mac-intel ./cmd/proxy

build-windows-amd64:
	GOOS=windows GOARCH=amd64 go build -o $(APP_NAME)-windows-amd64.exe ./cmd/proxy

clean:
	rm -f $(APP_NAME) $(APP_NAME)-* *.exe

run:
	go run ./cmd/proxy
