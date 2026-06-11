# Stage 1: Build the Go application
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Install git if needed (some modules require it to download)
RUN apk add --no-cache git

ENV GOPROXY=https://goproxy.cn,direct

COPY go.mod ./
COPY . .

RUN go mod tidy && go mod download

# Compile a CGO-free statically linked binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o kickrss main.go

# Stage 2: Create a minimal runner image
FROM alpine:3.18

ENV TZ=Asia/Shanghai
ENV PORT=8888
ENV CONFIG_PATH=/app/data/config.yaml
ENV DB_PATH=/app/data/myrss.db

WORKDIR /app

# Install basic diagnostic tools and timezone support
RUN apk add --no-cache tzdata ca-certificates sqlite curl

COPY --from=builder /app/kickrss /app/kickrss
COPY static /app/static
COPY config.yaml.example /app/config.yaml.example
COPY docker-entrypoint.sh /app/docker-entrypoint.sh

RUN chmod +x /app/docker-entrypoint.sh

EXPOSE 8888

ENTRYPOINT ["/app/docker-entrypoint.sh"]
