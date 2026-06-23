.PHONY: build build-web build-linux clean secret-scan bench-list bench-setup bench-run bench-score

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

# --- benchmarks ---
BENCH ?= swebench
MODEL ?= deepseek-v4-pro
CONFIG ?= $$HOME/.cece/settings.json
CECE_BIN ?= ./bin/cece-linux-amd64

bench-list:
	python -m benchmarks list

bench-setup:
	python -m benchmarks setup --benchmark $(BENCH)

bench-build: build-linux
	@echo "Binary ready for benchmarks: $(CECE_BIN)"

bench-run: build-linux
	python -m benchmarks run $(BENCH) \
		--model $(MODEL) \
		--config $(CONFIG) \
		--cece-bin $(CECE_BIN) \
		--max-workers 1 \
		--timeout 600 \
		--slice :1

bench-score:
	python -m benchmarks score $(BENCH)
