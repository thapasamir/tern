# One Dockerfile for every Tern process. Build the one you want with --target:
#   docker build --target api    -t tern-api    backend
#   docker build --target worker -t tern-worker backend
#   docker build --target migrate -t tern-migrate backend

FROM golang:1.26-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0

# Dependencies first, so editing code does not re-download them.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
 && go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker
RUN go install github.com/pressly/goose/v3/cmd/goose@v3.24.1

# Shared base: no shell tooling beyond what is needed, runs as an unprivileged user.
FROM alpine:3.21 AS runtime
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -u 10001 tern
USER tern

FROM runtime AS api
COPY --from=build /out/api /usr/local/bin/api
EXPOSE 8080
ENTRYPOINT ["api"]

FROM runtime AS worker
COPY --from=build /out/worker /usr/local/bin/worker
EXPOSE 9102
ENTRYPOINT ["worker"]

# Applies the database migrations and exits: goose reads GOOSE_DRIVER and GOOSE_DBSTRING.
FROM runtime AS migrate
COPY --from=build /go/bin/goose /usr/local/bin/goose
COPY migrations /migrations
ENTRYPOINT ["goose", "-dir", "/migrations", "up"]
