# syntax=docker/dockerfile:1
# ---- 构建阶段 ----
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
RUN go build -trimpath -ldflags="-s -w" -o /out/units ./cmd/units

# ---- 运行阶段 ----
FROM alpine:3.20
RUN adduser -D -u 10001 appuser
COPY --from=build /out/units /usr/local/bin/units
USER appuser
EXPOSE 8080
ENV ADDR=":8080"
HEALTHCHECK --interval=10s --timeout=3s --retries=5 \
  CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/units"]
