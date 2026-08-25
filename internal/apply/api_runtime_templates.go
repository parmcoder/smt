package apply

const apiContainerfile = `FROM golang:1.26.5-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH
RUN target_arch="${TARGETARCH:-$(go env GOARCH)}" && \
    CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${target_arch}" \
    go build -p=1 -buildvcs=false -trimpath -ldflags="-s -w" -o /out/apis .

FROM alpine:3.22

RUN addgroup -S -g 10001 smt && adduser -S -D -H -u 10001 -G smt smt
COPY --from=builder /out/apis /app/apis
WORKDIR /app
USER 10001:10001
EXPOSE 8080
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=5s --timeout=2s --start-period=2s --retries=6 CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz && wget -q -O /dev/null http://127.0.0.1:8080/readyz
ENTRYPOINT ["/app/apis"]
`
