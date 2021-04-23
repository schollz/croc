FROM golang:1.16-alpine as builder
RUN apk add --no-cache git
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
ENTRYPOINT ["/croc-entrypoint.sh"]
CMD ["relay"]
