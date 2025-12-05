FROM --platform=$BUILDPLATFORM golang:1.25.1 AS build

WORKDIR /src

COPY go.mod go.sum .
RUN go mod download && go mod verify
COPY . .

ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /src/vk-proxy .

FROM debian:13.2-slim

RUN apt update && apt install -y ca-certificates zbar-tools

RUN mkdir -p /usr/local/bin /usr/local/etc/vk-proxy /var/log/vk-proxy
RUN touch /usr/local/etc/vk-proxy/config.json /var/log/vk-proxy/output.log

COPY --from=build /src/vk-proxy /usr/local/bin/vk-proxy

EXPOSE 1080

ENTRYPOINT ["/usr/local/bin/vk-proxy"]
CMD ["-config", "/usr/local/etc/vk-proxy/config.json"]
