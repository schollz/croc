# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

RUN apk add --no-cache git nodejs npm

WORKDIR /go/croc

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN npm ci --prefix web \
    && npm run embed --prefix web

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
RUN if [ -n "$TARGETVARIANT" ]; then export GOARM="${TARGETVARIANT#v}"; fi \
    && CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" \
       go build -buildvcs=false -trimpath -tags netgo,osusergo \
       -ldflags="-s -w -buildid=" -o /out/croc . \
    && CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" \
       go build -buildvcs=false -trimpath -tags netgo,osusergo \
       -ldflags="-s -w -buildid=" -o /out/croc-web ./cmd/croc-web

FROM alpine:latest

EXPOSE 8080
EXPOSE 9009
EXPOSE 9010
EXPOSE 9011
EXPOSE 9012
EXPOSE 9013
EXPOSE 9014
EXPOSE 9015
EXPOSE 9016
EXPOSE 9017

COPY --from=builder /out/croc /out/croc-web /go/croc/croc-entrypoint.sh /

RUN mkdir -p /www/croc/storage \
    && chown -R nobody:nobody /www/croc

USER nobody

# Simple TCP health check with nc!
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD sh -c ' \
    P="${CROC_RELAY_PORTS:-${CROC_PORTS:-${CROC_PORT:-9009}}}"; \
    IFS=,; set -- $P; \
    for p in "$@"; do \
        nc -z -w 3 localhost "$p" || exit 1; \
    done'

ENTRYPOINT ["/croc-entrypoint.sh"]
CMD ["relay"]
