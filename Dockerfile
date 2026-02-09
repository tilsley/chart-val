FROM golang:1.24-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /chart-val ./cmd/chart-val

FROM alpine:3.19

RUN apk add --no-cache ca-certificates helm && \
    adduser -D -h /app appuser

USER appuser
WORKDIR /app
COPY --from=builder /chart-val .

EXPOSE 8080
ENTRYPOINT ["./chart-val"]
