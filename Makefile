.PHONY: help build build-linux build-all test test-verbose test-race test-cover clean tidy fmt vet run

# Default target
help:
	@echo "Available targets:"
	@echo "  build         - Build the application binary for current platform"
	@echo "  build-linux   - Build the application binary for Linux AMD64"
	@echo "  build-all     - Build binaries for both macOS and Linux"
	@echo "  test          - Run unit tests"
	@echo "  test-verbose  - Run unit tests with verbose output"
	@echo "  test-race     - Run unit tests with race detection"
	@echo "  test-cover    - Run unit tests with coverage report"
	@echo "  tidy          - Run go mod tidy to clean up dependencies"
	@echo "  fmt           - Format code with go fmt"
	@echo "  vet           - Run go vet for static analysis"
	@echo "  clean         - Remove built binaries and coverage files"
	@echo "  run           - Build and run the application"
	@echo "  all           - Run fmt, vet, tidy, and test"

# Build the application for current platform
build:
	go build -o steam main.go

# Build the application for Linux AMD64
build-linux:
	GOOS=linux GOARCH=amd64 go build -o steam-linux main.go

# Build for both platforms
build-all: build build-linux

# Run unit tests
test:
	go test -cover

# Run unit tests with verbose output
test-verbose:
	go test -v -cover

# Run unit tests with race detection
test-race:
	go test -v -race -cover

# Run unit tests with coverage report
test-cover:
	go test -coverprofile=coverage.out
	go tool cover -func=coverage.out

# Run go mod tidy
tidy:
	go mod tidy

# Format code
fmt:
	go fmt ./...

# Run go vet
vet:
	go vet ./...

# Clean build artifacts
clean:
	rm -f steam steam-linux
	rm -f coverage.out

# Build and run
run: build
	./steam

# Run all checks
all: fmt vet tidy test
