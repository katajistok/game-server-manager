FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum* ./
RUN go mod download

COPY agent.go main.go docker.go metrics.go protocol.go rcon.go ./

RUN CGO_ENABLED=0 GOOS=linux go build -o agent .

FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/agent .

CMD ["./agent"]
