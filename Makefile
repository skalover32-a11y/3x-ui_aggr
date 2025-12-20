DB_DSN ?= postgres://agg:agg@localhost:5432/agg?sslmode=disable

.PHONY: run migrate-up migrate-down migrate-status lint

run:
	cd backend && go run ./cmd/api

migrate-up:
	migrate -path backend/migrations -database "$(DB_DSN)" up

migrate-down:
	migrate -path backend/migrations -database "$(DB_DSN)" down 1

migrate-status:
	migrate -path backend/migrations -database "$(DB_DSN)" version

lint:
	cd backend && go vet ./...
