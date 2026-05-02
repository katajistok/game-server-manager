FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod ./
RUN go mod tidy || true

COPY . .
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -o master .

FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/master .

EXPOSE 8080

CMD ["./master"]
