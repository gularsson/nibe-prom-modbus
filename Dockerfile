FROM golang:1.23.4 AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o main ./cmd/main.go

FROM alpine:latest
WORKDIR /root/

COPY --from=builder /app/main .
EXPOSE 2112

ENTRYPOINT ["./main"]

CMD ["--host=0.0.0.0", "--port=2112", "--path=/metrics"]
