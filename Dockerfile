FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./
COPY internal/ ./internal/
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /cross-seed-cleanup

FROM gcr.io/distroless/static-debian13

COPY --from=builder /cross-seed-cleanup /cross-seed-cleanup
USER nonroot:nonroot
ENTRYPOINT ["/cross-seed-cleanup"]
