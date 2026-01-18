FROM golang:1.23-alpine AS builder
WORKDIR /build
COPY . .
ENV CGO_ENABLED=0
RUN GOOS=linux go build -trimpath -ldflags="-s -w" -o mita cmd/mita/mita.go

FROM alpine AS base
COPY --from=builder /build/mita /usr/local/bin/
WORKDIR /app
RUN adduser -H -D -g "" mita && \
    mkdir -p /etc/mita && \
    chown -R mita:mita /etc/mita && \
    chmod 775 /etc/mita && \
    mkdir -p /var/lib/mita && \
    chown -R mita:mita /var/lib/mita && \
    chmod 775 /var/lib/mita && \
    mkdir -p /var/run/mita && \
    chown -R mita:mita /var/run/mita && \
    chmod 775 /var/run/mita
