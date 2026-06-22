.PHONY: build build-web build-linux clean secret-scan

WEBAPP_DIR := internal/observatory/webapp

build-web:
	@echo "Building observatory webapp..."
	npm --prefix $(WEBAPP_DIR) ci
	npm --prefix $(WEBAPP_DIR) run build

build: build-web
	@echo "Building cece..."
	go build -o cece ./cmd/cece
	@echo "→ cece"

# Cross-compile cece for Linux (SWE-bench containers are x86_64 Linux)
build-linux: build-web
	@echo "Building cece for linux/amd64..."
	GOOS=linux GOARCH=amd64 go build -o bin/cece-linux-amd64 ./cmd/cece
	@echo "→ bin/cece-linux-amd64"

clean:
	rm -f cece bin/cece-linux-amd64

secret-scan:
	./scripts/secret-scan.sh
