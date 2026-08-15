.PHONY: tidy test build run-api run-collector compose-up

tidy:
	go mod tidy

test:
	go test ./internal/eis/...
	@if [ -n "$$DATABASE_URL" ]; then go test ./...; else echo "skip DB tests (set DATABASE_URL)"; fi

build:
	go build -o bin/api ./cmd/api
	go build -o bin/collector ./cmd/collector

run-api:
	go run ./cmd/api

run-collector:
	go run ./cmd/collector

compose-up:
	docker compose up --build
