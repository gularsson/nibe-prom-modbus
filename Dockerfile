FROM golang:1.25.5 AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o main ./cmd/main.go

# Static binary with no file, TLS or tzdata needs: distroless/static is enough.
# The :nonroot variant runs as 65532, and the binary sits outside /root so a
# non-root user can exec it (the old alpine image used WORKDIR /root/, mode
# 0700, which forced the pod to run as uid 0).
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

COPY --from=builder /app/main /usr/local/bin/nibe-prom-modbus
USER 65532:65532
EXPOSE 2112

ENTRYPOINT ["/usr/local/bin/nibe-prom-modbus"]

CMD ["--host=0.0.0.0", "--port=2112", "--path=/metrics"]
