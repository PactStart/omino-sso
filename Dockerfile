# syntax=docker/dockerfile:1

# ---------- build ----------
FROM golang:1.26-alpine AS build
WORKDIR /src

# 先只拷依赖清单，命中缓存时无需重复下载模块
COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY configs ./configs

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/sso ./cmd/sso

# ---------- runtime ----------
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 sso
WORKDIR /app

COPY --from=build /out/sso /app/sso
# FR-012：验证码邮件模板必须随镜像发布；缺失时 MAIL_TEMPLATE_PATH 会在运行时打开失败
COPY --from=build /src/configs/verify-mail.tmpl /app/configs/verify-mail.tmpl

ENV MAIL_TEMPLATE_PATH=/app/configs/verify-mail.tmpl

USER sso
EXPOSE 8080 8081
ENTRYPOINT ["/app/sso"]
