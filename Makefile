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

# Build cece for Linux arm64 (Apple Silicon native)
build-linux-arm64: build-web
	@echo "Building cece for linux/arm64..."
	GOOS=linux GOARCH=arm64 go build -o bin/cece-linux-arm64 ./cmd/cece
	@echo "→ bin/cece-linux-arm64"

clean:
	rm -f cece bin/cece-linux-amd64 bin/cece-linux-arm64

secret-scan:
	./scripts/secret-scan.sh

# --- benchmarks ---
BENCH ?= swebench
MODEL ?= glm-5v
CECE_BIN ?= ./bin/cece-linux-arm64

bench-list:
	python -m benchmarks list

bench-setup:
	python -m benchmarks setup --benchmark $(BENCH)

bench-build: build-linux-arm64
	@echo "Binary ready for benchmarks: $(CECE_BIN)"

bench-build-images:
	python -m benchmarks.build_images --slice :10

bench-run:
	python3 -m benchmarks run $(BENCH) \
		--model $(MODEL) \
		--concurrency 1 \
		--timeout 600 \
		--slice :1

bench-score:
	@if [ "$(BENCH)" = "swebench" ]; then \
		echo "SWE-bench is scored inline by: make bench-run BENCH=swebench"; \
		exit 1; \
	fi
	python -m benchmarks score $(BENCH)
