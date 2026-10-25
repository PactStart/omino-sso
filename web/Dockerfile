# syntax=docker/dockerfile:1

# 前端镜像：portal / admin 两个 target（同一 commit、同一 tag，产物各自独立）
#
# 构建上下文必须是 ./web（不是仓库根），因此只能看到 portal/、admin/、nginx/：
#
#   docker build -f web/Dockerfile --target portal \
#     --build-arg VITE_ADMIN_HOME=https://admin.example.com/ \
#     --build-arg VITE_PORTAL_HOME=https://portal.example.com/account/profile \
#     --build-arg VITE_PORTAL_LOGIN_URL=https://portal.example.com/login \
#     --build-arg VITE_SITE_NAME="统一身份认证" \
#     -t oms-portal:2026.10.23-1 ./web
#
# 环境差异只有两处：构建期 VITE_*（前端产物）与运行时 API_UPSTREAM（nginx 反代目标）。
# 镜像内**不含**任何环境域名：域名来自构建参数与运行时 env，因此同一份代码可跨环境复用。

ARG NODE_IMAGE=node:22-alpine
ARG NGINX_IMAGE=nginx:1.27-alpine

# ---------- 依赖层（两套前端共用一张缓存层） ----------
FROM ${NODE_IMAGE} AS deps
WORKDIR /web
# 只拷依赖清单：源码改动不会打掉 node_modules 缓存
COPY portal/package.json portal/package-lock.json ./portal/
COPY admin/package.json admin/package-lock.json ./admin/
RUN cd portal && npm ci && cd ../admin && npm ci

# ---------- portal 构建 ----------
FROM deps AS build-portal
# 门户登录后跳转的后台入口（必填：缺失会让「登录成功却进不了后台」）
ARG VITE_ADMIN_HOME
# 以下三项与 admin 共用同一组值，仅供代码内兜底使用
ARG VITE_PORTAL_HOME
ARG VITE_PORTAL_LOGIN_URL
ARG VITE_SITE_NAME
# 统一身份登录域（必填：缺失会让门户丢弃未登录 SSO 的回跳，用户"登录成功却回不到应用"）
ARG VITE_SSO_BASE_URL
# VITE_API_BASE_URL 刻意不传：保持代码内默认 /api/v1（同源反代）
RUN test -n "$VITE_ADMIN_HOME" || { \
      echo "缺少 --build-arg VITE_ADMIN_HOME（示例：https://admin.example.com/users）"; exit 1; }
RUN test -n "$VITE_SSO_BASE_URL" || { \
      echo "缺少 --build-arg VITE_SSO_BASE_URL（示例：https://sso.example.com）"; exit 1; }
ENV VITE_ADMIN_HOME=${VITE_ADMIN_HOME} \
    VITE_PORTAL_HOME=${VITE_PORTAL_HOME} \
    VITE_PORTAL_LOGIN_URL=${VITE_PORTAL_LOGIN_URL} \
    VITE_SSO_BASE_URL=${VITE_SSO_BASE_URL} \
    VITE_SITE_NAME=${VITE_SITE_NAME}
COPY portal/ ./portal/
# build = vue-tsc --noEmit && vite build，类型错误会让镜像构建失败（不产出坏产物）
RUN cd portal && npm run build

# ---------- admin 构建 ----------
FROM deps AS build-admin
# 后台「返回门户」与「会话失效回登录页」（必填：缺失会跳到错误环境）
ARG VITE_PORTAL_HOME
ARG VITE_PORTAL_LOGIN_URL
ARG VITE_ADMIN_HOME
ARG VITE_SITE_NAME
RUN test -n "$VITE_PORTAL_HOME" -a -n "$VITE_PORTAL_LOGIN_URL" || { \
      echo "缺少 --build-arg VITE_PORTAL_HOME / VITE_PORTAL_LOGIN_URL"; exit 1; }
ENV VITE_PORTAL_HOME=${VITE_PORTAL_HOME} \
    VITE_PORTAL_LOGIN_URL=${VITE_PORTAL_LOGIN_URL} \
    VITE_ADMIN_HOME=${VITE_ADMIN_HOME} \
    VITE_SITE_NAME=${VITE_SITE_NAME}
COPY admin/ ./admin/
RUN cd admin && npm run build

# ---------- portal 运行时 ----------
# nginx 官方镜像自带 /docker-entrypoint.d/20-envsubst-on-templates.sh：
# 把 /etc/nginx/templates/*.template 渲染到 /etc/nginx/conf.d/*.conf（只替换已定义的环境变量）。
# 禁止裸 `envsubst < tpl > conf`——那会把 $host / $uri / $remote_addr 一起清空。
FROM ${NGINX_IMAGE} AS portal
# 官方镜像自带 /etc/nginx/conf.d/default.conf（server_name localhost），
# 它是 80 端口的默认 server，不清掉会导致所有请求都落到它身上 → 全站 404
RUN rm -f /etc/nginx/conf.d/default.conf
COPY --from=build-portal /web/portal/dist/ /usr/share/nginx/html/
COPY nginx/templates/portal.conf.template /etc/nginx/templates/portal.conf.template
# API_UPSTREAM 由编排注入（一体化 http://sso:8081，test/prod 内网 api 域）。
# 这里给的回环默认值只为让 `docker run --rm <镜像> nginx -t` 这类镜像自检能通过：
# 模板变量未替换时 nginx 会报 "unknown api_upstream variable"（难懂），
# 而默认值下未注入只表现为 /api/ 502 + 错误日志 connection refused。
# 三种环境都由编排显式注入，不依赖本默认值。
ENV API_UPSTREAM=http://127.0.0.1:8081
EXPOSE 80
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1/healthz || exit 1

# ---------- admin 运行时 ----------
FROM ${NGINX_IMAGE} AS admin
RUN rm -f /etc/nginx/conf.d/default.conf
COPY --from=build-admin /web/admin/dist/ /usr/share/nginx/html/
COPY nginx/templates/admin.conf.template /etc/nginx/templates/admin.conf.template
# 同 portal：默认值仅服务镜像自检，编排必须显式注入
ENV API_UPSTREAM=http://127.0.0.1:8081
EXPOSE 80
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1/healthz || exit 1
