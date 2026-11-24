FROM golang:1.27-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/agentverdicts ./cmd/agentverdicts
# Pre-create the secrets dir owned by the non-root UID the runtime stage runs
# as (scratch has no chown), so a fresh named volume inherits the ownership.
RUN mkdir -p /appsecrets && chown 65532:65532 /appsecrets

FROM scratch AS production
COPY --from=builder /out/agentverdicts /agentverdicts
COPY --from=builder --chown=65532:65532 /appsecrets /secrets
USER 65532:65532
ENV AGENTVERDICTS_HTTP_ADDR=:8080 AGENTVERDICTS_SECRET_DIR=/secrets
EXPOSE 8080
ENTRYPOINT ["/agentverdicts"]

# Dev only: Go toolchain + Air; source arrives via bind mount (see compose.yaml).
FROM golang:1.27-alpine AS dev
RUN go install github.com/air-verse/air@v1.67.4
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
CMD ["air", "-c", "/etc/air.toml"]
