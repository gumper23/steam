.PHONY: help build test test-verbose test-race test-cover clean tidy fmt vet run

# Default target
help:
	@echo "Available targets:"
	@echo "  build         - Build the application binary"
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

# Build the application
build:
	go build -o steam main.go

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
	rm -f steam
	rm -f coverage.out

# Build and run
run: build
	./steam

# Run all checks
all: fmt vet tidy test
