# Build stage
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" \
    -o /bin/netdata-postgres-mcp \
    ./cmd/netdata-postgres-mcp

# Runtime stage
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /bin/netdata-postgres-mcp /usr/local/bin/netdata-postgres-mcp

ENTRYPOINT ["netdata-postgres-mcp"]
CMD ["run"]
