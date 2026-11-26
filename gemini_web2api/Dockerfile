# syntax=docker/dockerfile:1

# Home Assistant Supervisor 2026.04+ no longer injects BUILD_FROM by default.
# Use the official multi-architecture Home Assistant base image explicitly.
ARG BUILD_VERSION=4.35.0

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS builder
ARG TARGETARCH
ARG TARGETOS=linux
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" \
    -o /out/gemini-web2api .

FROM ghcr.io/home-assistant/base:3.24

ARG BUILD_VERSION
ARG BUILD_ARCH

LABEL io.hass.version="${BUILD_VERSION}" \
      io.hass.type="app" \
      io.hass.arch="${BUILD_ARCH}"

# su-exec lets the small root entrypoint prepare /data, then drop privileges
# before starting the Go service.
RUN apk add --no-cache jq bash sqlite su-exec python3 \
    && addgroup -S -g 10001 gemini \
    && adduser -S -D -H -u 10001 -G gemini gemini

COPY --from=builder /out/gemini-web2api /usr/bin/gemini-web2api
COPY adapter /opt/gemini-image-adapter
COPY run.sh /run.sh
RUN chmod 0755 /run.sh

VOLUME ["/data"]
EXPOSE 8083/tcp 8084/tcp

CMD ["/run.sh"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD (wget -qO- http://127.0.0.1:8083/health && wget -qO- http://127.0.0.1:8084/health) || exit 1
