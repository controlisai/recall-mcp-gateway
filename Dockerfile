FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o recall-mcp-gateway ./cmd/gateway/

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
RUN adduser -D -u 1000 gateway
COPY --from=builder /app/recall-mcp-gateway /usr/local/bin/recall-mcp-gateway
COPY --from=builder /app/configs/standalone.yaml /app/configs/standalone.yaml
COPY --from=builder /app/configs/postgres.yaml /app/configs/postgres.yaml
COPY --from=builder /app/configs/multinode.yaml /app/configs/multinode.yaml
COPY --from=builder /app/docker-compose.yaml /app/docker-compose.yaml
RUN mkdir -p /data /home/gateway/.recall-mcp-gateway && chown -R gateway:gateway /app /data /home/gateway/.recall-mcp-gateway
USER gateway
WORKDIR /app

# Default audit storage to /data (persistent volume mount point)
ENV RECALL_AUDIT_PATH=/data/audit.db

VOLUME ["/data"]
EXPOSE 8090
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- http://localhost:8090/healthz || exit 1
ENTRYPOINT ["recall-mcp-gateway"]
CMD []
