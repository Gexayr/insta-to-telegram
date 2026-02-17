FROM golang:1.24-alpine AS builder

WORKDIR /app

RUN apk add --no-cache gcc musl-dev

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o app main.go

FROM alpine:latest

WORKDIR /app

RUN mkdir -p /app/data

COPY --from=builder /app/app .

CMD ["./app"]