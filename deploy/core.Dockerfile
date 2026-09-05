# Deployable 1 of ADR-0017: compile the core, then ship the binary and nothing else.

FROM golang:1.27-alpine AS build

WORKDIR /src

# Dependencies first, so a change in the code does not download the module graph again.
COPY src/core/go.mod src/core/go.sum ./
RUN go mod download

COPY src/core ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/core .

FROM alpine:3.22

# Certificates for the outgoing calls to Meta, and a user that is not root.
RUN apk add --no-cache ca-certificates \
    && adduser -D -u 10001 sonia

COPY --from=build /out/core /usr/local/bin/core

USER sonia
EXPOSE 8080

ENTRYPOINT ["core"]
