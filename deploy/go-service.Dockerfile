# Multi-stage build for any Go service in this monorepo.
# Usage: build with --build-arg SERVICE=<service-dir-name>.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY packages ./packages
COPY services ./services
ARG SERVICE
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/app ./services/${SERVICE}

FROM alpine:3.20
RUN adduser -D -u 10001 app
USER app
COPY --from=build /out/app /app
ENTRYPOINT ["/app"]
