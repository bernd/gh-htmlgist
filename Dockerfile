FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /htmlgist-proxy ./cmd/htmlgist-proxy/

FROM alpine:3.20

RUN apk add --no-cache ca-certificates
COPY --from=build /htmlgist-proxy /usr/local/bin/htmlgist-proxy

EXPOSE 8080
ENTRYPOINT ["htmlgist-proxy"]
