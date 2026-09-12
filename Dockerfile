FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/infrawho ./cmd/infrawho

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/infrawho /usr/local/bin/infrawho
EXPOSE 8080
# Default: run as root so lab bind-mounted master.key files (often host-owned
# mode 0600) remain readable. Do not add a fixed non-root USER here — that
# breaks typical lab key mounts. For production, run with a matching UID, e.g.
#   docker run --user "$(stat -c %u:%g /etc/infrawho/master.key)" …
# or mount the key via Docker/Podman secrets readable by that user.
ENTRYPOINT ["/usr/local/bin/infrawho"]
