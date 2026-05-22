FROM golang:1.25-alpine AS build
WORKDIR /src

RUN apk add --no-cache nodejs npm

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go install github.com/a-h/templ/cmd/templ@latest
RUN $(go env GOPATH)/bin/templ generate

RUN npm install --no-audit --no-fund
RUN npx tailwindcss -i assets/css/input.css -o assets/static/app.css --minify

RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/server ./cmd/server
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/migrate ./cmd/migrate

# The final image runs the app, which shells out to pg_restore/psql during
# drills — so it needs the PostgreSQL client tools, plus CA certificates
# for outbound TLS (Stripe, Postmark, Neon, S3, ...).
FROM alpine:3.21
RUN apk add --no-cache postgresql17-client ca-certificates
WORKDIR /app
COPY --from=build /out/server /app/server
COPY --from=build /out/migrate /app/migrate
COPY --from=build /src/assets/static /app/assets/static
COPY --from=build /src/migrations /app/migrations
EXPOSE 8080
ENTRYPOINT ["/app/server"]
