run:
	go run ./cmd/lead-log

fmt:
	gofmt -w ./cmd/lead-log ./internal ./pkg

tidy:
	go mod tidy

up:
	docker compose up -d

down:
	docker compose down