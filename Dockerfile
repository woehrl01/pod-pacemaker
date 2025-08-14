FROM --platform=${BUILDPLATFORM:-linux/amd64} golang:1.25-alpine AS builder

ARG BUILDPLATFORM
ARG TARGETOS
ARG TARGETARCH

RUN apk --no-cache add ca-certificates make
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN make build TARGETOS=${TARGETOS} TARGETARCH=${TARGETARCH}

FROM --platform=${BUILDPLATFORM:-linux/amd64} scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
WORKDIR /root/
COPY --from=builder /app/bin .
CMD ["./cni-init"]
