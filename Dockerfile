FROM golang:1.22-alpine AS builder
RUN apk --no-cache add ca-certificates make
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN make build


FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
WORKDIR /root/
COPY --from=builder /app/bin .
CMD ["./cni-init"]
