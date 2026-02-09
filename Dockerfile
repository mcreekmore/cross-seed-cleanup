FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /cross-seed-cleanup

FROM alpine:3.21

COPY --from=builder /cross-seed-cleanup /cross-seed-cleanup

ENTRYPOINT ["/cross-seed-cleanup"]
