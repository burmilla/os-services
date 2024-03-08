# First stage: Build static binary
FROM golang:1.21-alpine as builder
RUN apk add -U --no-cache ca-certificates
WORKDIR /go/src/azidentity
COPY . .
RUN CGO_ENABLED=0 go build -o azidentity

# Second stage: setup the runtime container
FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /go/src/azidentity/azidentity .
CMD ["./azidentity"]
