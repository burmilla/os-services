FROM golang:1.24-alpine AS builder
ARG VERSION=dev
WORKDIR /go/src/fleetclient
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.Version=${VERSION}" -o fleet-client

# ros and system-docker come from the host through command-volumes
FROM alpine:3.22
COPY --from=builder /go/src/fleetclient/fleet-client /usr/bin/fleet-client
CMD ["/usr/bin/fleet-client"]
