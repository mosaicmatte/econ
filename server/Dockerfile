FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o econ-server .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/econ-server .
COPY data/ ./data/
EXPOSE 8080
CMD ["./econ-server"]
