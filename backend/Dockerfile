FROM golang:1.23-alpine AS build
WORKDIR /app
RUN apk add --no-cache git
ARG TARGETOS=linux
ARG TARGETARCH=amd64
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /bin/api ./cmd/api
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /bin/vlf-agent ./cmd/node-agent

FROM alpine:3.20
RUN apk add --no-cache putty curl
RUN mkdir -p /app/bin /app/deploy/agent
RUN adduser -D -H -u 10001 appuser
USER appuser
WORKDIR /app
COPY --from=build /bin/api /app/api
COPY --from=build /bin/vlf-agent /app/bin/vlf-agent
COPY --from=build /app/deploy/agent/vlf-agent.service /app/deploy/agent/vlf-agent.service
EXPOSE 8080
ENTRYPOINT ["/app/api"]
