FROM golang:1.12-alpine as builder
RUN apk add --no-cache git
WORKDIR /go/croc
COPY . .
RUN go build -v

FROM alpine:latest 
EXPOSE 9009
EXPOSE 9010
EXPOSE 9011
EXPOSE 9012
EXPOSE 9013
COPY --from=builder /go/croc/croc /croc
CMD ["sh", "-c", "/croc --pass $CROC_PASS relay"]
