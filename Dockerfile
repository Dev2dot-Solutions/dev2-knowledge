FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -o /server ./cmd/server

FROM alpine:3.19
RUN apk --no-cache add ca-certificates tzdata git openssh-client
WORKDIR /app
COPY --from=builder /server .
# Rust parser sidecar used by the ingestion pipeline.
# Requires the dev2-solutions/ingest-parser:latest image to be built first
# (from the ingest-parser repo: docker build -t dev2-solutions/ingest-parser:latest .)
COPY --from=dev2-solutions/ingest-parser:latest /ingest-parser /usr/local/bin/ingest-parser
EXPOSE 8080
ENTRYPOINT ["/app/server"]
