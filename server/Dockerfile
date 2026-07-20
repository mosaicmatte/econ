FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o econ-server .

FROM alpine:latest
# tzdata: schedule-driven control (the APLC plug sweep's work-hours window) must run in
# BUILDING-local time. Without zone data the container falls back to UTC and "after
# hours" lands 7 hours off in Vietnam — armed at lunch, disarmed at midnight.
RUN apk add --no-cache tzdata
WORKDIR /app
COPY --from=builder /app/econ-server .
COPY data/ ./data/
EXPOSE 8080
CMD ["./econ-server"]
