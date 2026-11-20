FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
ARG VERSION=0.0.4-dev
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /orbita ./cmd/orbita-cli

FROM debian:bookworm-slim
RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      exim4-daemon-light logrotate openssl ca-certificates tzdata tini \
 && rm -rf /var/lib/apt/lists/* \
 && ln -s /usr/local/bin/orbita /usr/local/bin/orbita-cli
COPY --from=build /orbita /usr/local/bin/orbita
EXPOSE 25 587
VOLUME ["/data", "/var/spool/exim4"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=40s CMD ["/usr/local/bin/orbita", "health"]
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/orbita"]
CMD ["run"]
