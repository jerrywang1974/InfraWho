FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/infrawho ./cmd/infrawho

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/infrawho /usr/local/bin/infrawho
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/infrawho"]
