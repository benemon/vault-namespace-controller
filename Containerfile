# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ cmd/
COPY pkg/ pkg/

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -a -o vault-namespace-controller cmd/controller/main.go

# Final stage using UBI 9 Micro
FROM registry.access.redhat.com/ubi9/ubi-micro

WORKDIR /

COPY --from=builder /workspace/vault-namespace-controller /vault-namespace-controller

RUN chgrp -R 0 /vault-namespace-controller && \
    chmod -R g=u /vault-namespace-controller && \
    chmod 755 /vault-namespace-controller

ENTRYPOINT ["/vault-namespace-controller"]
