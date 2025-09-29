FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git gcc musl-dev

WORKDIR /go/croc

COPY . .

RUN go build -v -ldflags="-s -w"

FROM alpine:latest

EXPOSE 9009
EXPOSE 9010
EXPOSE 9011
EXPOSE 9012
EXPOSE 9013

COPY --from=builder /go/croc/croc /go/croc/croc-entrypoint.sh /

USER nobody

# Simple TCP health check with nc!
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD nc -z localhost 9009 || exit 1

ENTRYPOINT ["/croc-entrypoint.sh"]
CMD ["relay"]
