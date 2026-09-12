# MiniKV — single static binary, minimal runtime image.
#
# Build:  docker build -t minikv .
# Run:    docker run --rm -p 8080:8080 -v minikv-data:/data minikv \
#           server --addr 0.0.0.0:8080 --data /data

FROM golang:1.22-alpine AS build

WORKDIR /src

# Cache module downloads (no external modules today, but keeps the pattern).
COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/minikv ./cmd/minikv

# ---
FROM alpine:3.20

# Run as an unprivileged user; the data volume must be writable by it.
RUN addgroup -S minikv && adduser -S -G minikv minikv

COPY --from=build /out/minikv /usr/local/bin/minikv

USER minikv
WORKDIR /data
EXPOSE 8080

# Health endpoint doubles as a container healthcheck target.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
  CMD wget -qO- http://127.0.0.1:8080/v1/status >/dev/null 2>&1 || exit 1

ENTRYPOINT ["minikv"]
CMD ["server", "--addr", "0.0.0.0:8080", "--data", "/data"]
