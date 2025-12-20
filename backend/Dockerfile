FROM golang:1.23-alpine AS build
WORKDIR /app
RUN apk add --no-cache git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/api ./cmd/api

FROM alpine:3.20
RUN apk add --no-cache putty
RUN adduser -D -H -u 10001 appuser
USER appuser
WORKDIR /app
COPY --from=build /bin/api /app/api
EXPOSE 8080
ENTRYPOINT ["/app/api"]
