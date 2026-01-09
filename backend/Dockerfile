# syntax=docker/dockerfile:1

FROM golang:1.23-alpine AS build
WORKDIR /app

RUN apk add --no-cache git

ARG TARGETOS=linux
ARG TARGETARCH=amd64

# ВАЖНО: контекст сборки = корень репо, поэтому go.mod/go.sum лежат в backend/
COPY backend/go.mod backend/go.sum ./
RUN go mod download

# Код бекенда
COPY backend/ ./

# Шаблоны деплоя агента (лежат в корне репо: deploy/agent)
COPY deploy/agent/ /app/deploy/agent/

# Сборка api и агента
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /bin/api ./cmd/api
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /bin/vlf-agent ./cmd/node-agent


FROM alpine:3.20

RUN apk add --no-cache putty curl
RUN adduser -D -H -u 10001 appuser
RUN mkdir -p /app/bin /app/deploy/agent

WORKDIR /app

COPY --from=build /bin/api /app/api
COPY --from=build /bin/vlf-agent /app/bin/vlf-agent
COPY --from=build /app/deploy/agent/ /app/deploy/agent/

# на всякий случай (скрипты)
RUN chmod +x /app/api /app/bin/vlf-agent /app/deploy/agent/*.sh 2>/dev/null || true

USER appuser
EXPOSE 8080
ENTRYPOINT ["/app/api"]
