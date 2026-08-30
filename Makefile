.PHONY: build run dev fmt lint tidy docker-build docker-up docker-down

build:
	go build -o bin/bot ./cmd/bot

run: build
	./bin/bot

# Auto-restart on file changes (installs air on first run)
dev:
	@test -f $(shell go env GOPATH)/bin/air || go install github.com/air-verse/air@latest
	@$(shell go env GOPATH)/bin/air

fmt:
	gofmt -w .

tidy:
	go mod tidy

lint:
	golangci-lint run ./...

docker-build:
	docker compose --profile docker build

docker-up:
	docker compose --profile docker up -d --build

docker-down:
	docker compose down
