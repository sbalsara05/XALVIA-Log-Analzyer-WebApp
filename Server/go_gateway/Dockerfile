# Stage 1: build
FROM golang:alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
ENV GOPROXY=https://proxy.golang.org,direct
RUN go mod download
COPY . .
ENV CGO_ENABLED=0
RUN go build -o gateway .

# Stage 2: test — Go toolchain stays in, tests run against a real DB
FROM golang:alpine AS test
WORKDIR /app
COPY go.mod go.sum ./
ENV GOPROXY=https://proxy.golang.org,direct
RUN go mod download
COPY . .
CMD ["go", "test", "ricovr/gateway/tests/integration", "-v", "-timeout", "60s"]

# Stage 3: minimal runtime
FROM alpine:latest
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/gateway .
COPY data/ ./data/
EXPOSE 8080
CMD ["./gateway"]
