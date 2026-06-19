FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o ecosync-server .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/ecosync-server .
COPY data/building-data.json ./data/
EXPOSE 8080
CMD ["./ecosync-server"]
