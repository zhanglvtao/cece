.PHONY: build-linux clean secret-scan

# Cross-compile cece for Linux (SWE-bench containers are x86_64 Linux)
build-linux:
	@echo "Building cece for linux/amd64..."
	GOOS=linux GOARCH=amd64 go build -o bin/cece-linux-amd64 ./cmd/cece
	@echo "→ bin/cece-linux-amd64"

clean:
	rm -f bin/cece-linux-amd64

secret-scan:
	./scripts/secret-scan.sh
