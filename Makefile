all: build test

build: build-orc build-api

build-orc:
	@echo "Building Orchestrator..."
	@go build -o bin/orchestrator cmd/orchestrator/main.go

build-api: 
	@echo "Building API..."
	@go build -o bin/api cmd/api/main.go

run-api:
	@go run cmd/api/main.go

run-orc:
	@go run cmd/orchestrator/main.go

seed:
	@go run cmd/seed/main.go

test:
	@echo "Testing..."
	@go test ./... -v

clean:
	@echo "Cleaning..."
	@rm -rf bin

watch:
	@if command -v air > /dev/null; then \
            air; \
            echo "Watching...";\
        else \
            read -p "Go's 'air' is not installed on your machine. Do you want to install it? [Y/n] " choice; \
            if [ "$$choice" != "n" ] && [ "$$choice" != "N" ]; then \
                go install github.com/air-verse/air@latest; \
                air; \
                echo "Watching...";\
            else \
                echo "You chose not to install air. Exiting..."; \
                exit 1; \
            fi; \
        fi

.PHONY: all build run test clean watch