# --- Build stage ---
FROM golang:1.25-alpine AS build

WORKDIR /src

# Cache dependencies separately from the source for faster rebuilds.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO is not needed: pgx and go-redis are pure Go, so we can produce a fully
# static binary that runs on a distroless/scratch base.
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/nexusvpn-api ./cmd/api

# --- Runtime stage ---
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/nexusvpn-api /usr/local/bin/nexusvpn-api

# REST, gRPC, metrics.
EXPOSE 8080 9090 9091

USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/nexusvpn-api"]
