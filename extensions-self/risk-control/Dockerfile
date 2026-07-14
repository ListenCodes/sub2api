FROM golang:1.26.5-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN GOPROXY=https://proxy.golang.org,direct go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/risk-control .

FROM alpine:3.22

RUN adduser -D -H -u 10001 risk-control
WORKDIR /app
COPY --from=builder /out/risk-control /app/risk-control
USER risk-control
EXPOSE 8090
ENTRYPOINT ["/app/risk-control"]
