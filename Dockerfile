# ---------- 构建阶段 ----------
# 基础镜像锁定版本(go.mod 要求 Go >= 1.23.2),可用 --build-arg 覆盖
ARG GO_VERSION=1.23
ARG ALPINE_VERSION=3.21
FROM golang:${GO_VERSION}-alpine AS builder

# 依赖代理可在构建时覆盖,国内网络示例:
#   docker build --build-arg GOPROXY=https://goproxy.cn,direct .
ARG GOPROXY=""
ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOPROXY=${GOPROXY:-https://proxy.golang.org,direct}

# 注入版本号(与 CI 发行版保持一致),示例:
#   docker build --build-arg VERSION=$(git describe --tags --always) .
ARG VERSION=dev

WORKDIR /build

# 先复制依赖清单,利用层缓存加速重复构建
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -trimpath -ldflags "-s -w -X 'go-geoip/common.Version=${VERSION}'" -o /go-geoip

# ---------- 运行阶段 ----------
FROM alpine:${ALPINE_VERSION}

ARG VERSION=dev
LABEL org.opencontainers.image.title="go-geoip" \
      org.opencontainers.image.version="${VERSION}"

# ca-certificates: HTTPS 下载数据库需要;tzdata: 时区
RUN apk --no-cache add ca-certificates tzdata

COPY --from=builder /go-geoip /go-geoip

# 数据库下载/存放目录(建议挂载卷持久化)
WORKDIR /app/go-geoip/data

EXPOSE 7099

# /ip 带鉴权,取 API_SECRET 第一段作为校验值;未设置 API_SECRET 时鉴权放行,同样可用
HEALTHCHECK --interval=30s --timeout=5s --start-period=2m --retries=3 \
    CMD wget -q -O /dev/null --header "Authorization: Bearer ${API_SECRET%%,*}" "http://127.0.0.1:${PORT:-7099}/ip" || exit 1

ENTRYPOINT ["/go-geoip"]
