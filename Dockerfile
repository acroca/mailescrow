FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /bin/mailescrow ./cmd/mailescrow

FROM alpine:3.20

RUN apk add --no-cache ca-certificates
COPY --from=builder /bin/mailescrow /usr/local/bin/mailescrow

EXPOSE 2525 8080

ENTRYPOINT ["mailescrow"]
