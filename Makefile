.PHONY: dev build run test lint tidy templ css migrate db-up db-down clean htmx evidence-dir

GOBIN := $(shell go env GOPATH)/bin
DATABASE_URL ?= postgres://soteria:soteria@localhost:5432/soteria?sslmode=disable

dev: db-up htmx templ css migrate evidence-dir
	DATABASE_URL=$(DATABASE_URL) go run ./cmd/server

evidence-dir:
	mkdir -p tmp/evidence

build: templ css
	mkdir -p bin
	go build -o bin/server ./cmd/server
	go build -o bin/migrate ./cmd/migrate

run:
	DATABASE_URL=$(DATABASE_URL) ./bin/server

test:
	go test ./...

lint:
	go vet ./...

tidy:
	go mod tidy

templ:
	$(GOBIN)/templ generate

htmx:
	@if [ ! -f assets/static/htmx.min.js ]; then \
		echo "fetching htmx.min.js..."; \
		curl -fsSL -o assets/static/htmx.min.js https://unpkg.com/htmx.org@2.0.3/dist/htmx.min.js; \
	fi

css:
	npx --yes tailwindcss \
		-i assets/css/input.css \
		-o assets/static/app.css \
		--minify

migrate:
	DATABASE_URL=$(DATABASE_URL) go run ./cmd/migrate up

db-up:
	docker compose up -d postgres
	@echo "Waiting for postgres..."
	@until docker compose exec -T postgres pg_isready -U soteria >/dev/null 2>&1; do sleep 1; done

db-down:
	docker compose down

clean:
	rm -rf bin tmp
	find . -name '*_templ.go' -delete
