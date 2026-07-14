FROM golang:1.26.5-alpine AS builder

WORKDIR /src/risk-control
COPY risk-control/go.mod risk-control/go.sum ./
RUN GOPROXY=https://proxy.golang.org,direct go mod download
COPY risk-control/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/extensions-self .

FROM alpine:3.22

RUN adduser -D -H -u 10001 extensions-self
WORKDIR /app
COPY --from=builder /out/extensions-self /app/extensions-self
COPY homepage/ /app/homepage/
ENV EXTENSIONS_SELF_HOMEPAGE_DIR=/app/homepage
USER extensions-self
EXPOSE 8090
ENTRYPOINT ["/app/extensions-self"]

