# The frontend is embedded into the binary, so it has to be built first and
# copied into the Go build context.
FROM node:22-alpine AS frontend

RUN corepack enable
WORKDIR /src

COPY internal/frontend/package.json internal/frontend/pnpm-lock.yaml internal/frontend/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

COPY internal/frontend/ ./
RUN pnpm build

FROM golang:1.26-alpine AS build

ARG VERSION=dev
ARG COMMIT=""
ARG BUILD_TIME=""
# Set by buildx when building multi-arch. Empty on a plain docker build, which
# then falls back to the builder's own architecture.
ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=frontend /src/dist ./internal/frontend/dist

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -trimpath \
    -ldflags "-s -w \
      -X arca/internal/version.Version=${VERSION} \
      -X arca/internal/version.Commit=${COMMIT} \
      -X arca/internal/version.BuildTime=${BUILD_TIME}" \
    -o /out/arca ./cmd/arca

# An empty named volume inherits the ownership of the image directory it is
# mounted over. Shipping /data owned by the runtime user is what makes
# `-v arca-data:/data` writable without a chown on the host.
RUN mkdir -p /data

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/arca /arca
COPY --from=build --chown=65532:65532 /data /data

# 65532 is nonroot in the distroless base image.
USER 65532:65532

ENV PORT=8080 \
    DATABASE_PATH=/data/arca.db \
    STORAGE_DIR=/data/storage

VOLUME ["/data"]
EXPOSE 8080

ENTRYPOINT ["/arca"]
