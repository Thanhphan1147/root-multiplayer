# syntax=docker/dockerfile:1

# ---- build stage ----
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/rmn-mp ./cmd/server

# ---- runtime stage ----
FROM alpine:3.20
RUN adduser -D -u 10001 rmn
COPY --from=build /out/rmn-mp /usr/local/bin/rmn-mp
COPY web /app/web
RUN mkdir -p /app/data && chown -R rmn /app
USER rmn
WORKDIR /app
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/api/health >/dev/null || exit 1
ENTRYPOINT ["rmn-mp", "--addr", ":8080", "--data", "/app/data", "--web", "/app/web"]
