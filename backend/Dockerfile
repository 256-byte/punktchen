# syntax=docker/dockerfile:1

# ---- build ----------------------------------------------------------------
FROM golang:1.27-alpine AS build
WORKDIR /src

# Dependencies first, so source changes do not invalidate the module cache layer.
COPY go.mod go.sum* ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags="-s -w" -o /out/livestop ./cmd/server

# ---- runtime --------------------------------------------------------------
# Static binary on distroless: no shell, no package manager, runs as non-root.
FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/livestop /livestop

# 8080: public API, 9090: metrics (task 0.3)
EXPOSE 8080 9090
USER nonroot:nonroot

HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --start-interval=1s --retries=3 \
    CMD ["/livestop", "healthcheck"]

ENTRYPOINT ["/livestop"]
