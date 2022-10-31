FROM golang:1.19 AS builder
WORKDIR /tmp/compile
COPY . .
RUN CGO_ENABLED=0 go build -v -ldflags="-s -w -X github.com/cockroachlabs/kitsune/internal/version/version/BuildVersion=$(git describe --tags --always --dirty)" -o /usr/bin/visus .

# Create a single-binary docker image, including a set of core CA
# certificates so that we can call out to any external APIs.
FROM scratch
WORKDIR /data/
ENTRYPOINT ["/usr/bin/visus"]
COPY --from=builder /usr/bin/visus /usr/bin/
