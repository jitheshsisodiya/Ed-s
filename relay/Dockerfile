# --- Build stage ---
FROM golang:1.25-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/relayd ./cmd/relayd

# --- Runtime stage ---
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/relayd /usr/local/bin/relayd

# UDP data plane and the Prometheus metrics endpoint.
EXPOSE 3479/udp 9092

USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/relayd"]
