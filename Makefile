.PHONY: run build test tidy docker-up docker-down

run:
	go run ./cmd/server

build:
	go build -o bin/server ./cmd/server

test:
	go test ./... -v

tidy:
	go mod tidy

docker-up:
	docker-compose up -d db
	@echo "MySQL started on port 3310. Waiting for ready..."
	@sleep 5

docker-down:
	docker-compose down

clean:
	rm -rf bin/
