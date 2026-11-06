# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/admin ./cmd/admin

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 app
WORKDIR /app
COPY --from=build /out/admin /app/admin
COPY asset /app/asset
RUN mkdir -p /app/data && chown -R app:app /app
USER app
ENV ADMIN_PORT=8081 \
    ADMIN_DB_PATH=/app/data/admin.db \
    ADMIN_ASSET_DIR=/app/asset
VOLUME ["/app/data"]
EXPOSE 8081
HEALTHCHECK --interval=30s --timeout=3s CMD wget -qO- http://127.0.0.1:8081/healthz || exit 1
ENTRYPOINT ["/app/admin"]
