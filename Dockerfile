# Build stage — multi-arch
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH

RUN apk add --no-cache git

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /bin/netdata-postgres-mcp \
    ./cmd/netdata-postgres-mcp

# Runtime stage — distroless for minimal attack surface
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /bin/netdata-postgres-mcp /usr/local/bin/netdata-postgres-mcp

USER nonroot:nonroot

EXPOSE 8765

ENTRYPOINT ["netdata-postgres-mcp"]
CMD ["run"]
