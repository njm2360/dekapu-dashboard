FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/watcher ./cmd/watcher
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/loader  ./cmd/loader

FROM alpine:3.23.3

WORKDIR /app

COPY --from=builder /out/watcher ./watcher
COPY --from=builder /out/loader  ./loader

CMD ["/app/watcher"]
