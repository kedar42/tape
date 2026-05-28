# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS build

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/gluetun-portwatch ./cmd/gluetun-portwatch

FROM alpine:3.23

RUN adduser -D -H -u 10001 portwatch

COPY --from=build /out/gluetun-portwatch /usr/local/bin/gluetun-portwatch

USER portwatch

ENTRYPOINT ["/usr/local/bin/gluetun-portwatch"]
