"""omino-sso 的 Python 接入示例：授权码 + PKCE（S256）。

只演示协议接入，不是产品代码：五个端点之外不暴露任何业务路由。
令牌只存在于服务端会话（session_store.py），浏览器只拿到 sid_py。

与 specs/008 的契约对齐：验证器用 Authlib 的 JsonWebToken + JWK Set 验签 id_token
（iss/aud/exp/nonce），token 交换与刷新用 Authlib 的 AsyncOAuth2Client
（client_secret_basic，即 HTTP Basic，契约要求）。
"""

from __future__ import annotations

import base64
import hashlib
import logging
import os
import secrets
import socket
import sys
import time
from contextlib import asynccontextmanager
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any
from urllib.parse import urlencode

import httpx
import uvicorn
from authlib.integrations.httpx_client import AsyncOAuth2Client
from authlib.jose import JsonWebToken
from authlib.oidc.core import CodeIDToken
from fastapi import FastAPI, Request
from fastapi.responses import HTMLResponse, JSONResponse, RedirectResponse, Response
from fastapi.templating import Jinja2Templates
from itsdangerous import BadSignature, SignatureExpired, URLSafeTimedSerializer

from session_store import Pending, RefreshRejected, Session, SessionStore

# cookie 名必须带语言前缀：三个示例与 IdP 同处 localhost，而 HTTP cookie 不按端口隔离，
# 用 sid 会覆盖 IdP 的会话（contracts/example-app-api.md）。
COOKIE_NAME = "sid_py"
SESSION_TTL = 12 * 3600
HTTP_TIMEOUT = 10.0
USERINFO_TIMEOUT = 5.0
SCOPES = "openid profile email offline_access"

LOG = logging.getLogger("examples.python")
BASE_DIR = Path(__file__).resolve().parent


# --------------------------------------------------------------------------- 配置


@dataclass(frozen=True)
class Config:
    issuer: str
    client_id: str
    client_secret: str
    redirect_uri: str
    session_secret: str
    port: int
    userinfo_override: str


def _fatal(message: str) -> None:
    print(f"[fatal] {message}", file=sys.stderr, flush=True)


def load_config() -> Config:
    """读 6 个环境变量并校验；缺项/非法一律非零退出（contracts「启动期约定」）。"""
    raw = {
        "SSO_ISSUER": os.getenv("SSO_ISSUER", "").strip(),
        "SSO_CLIENT_ID": os.getenv("SSO_CLIENT_ID", "").strip(),
        "SSO_CLIENT_SECRET": os.getenv("SSO_CLIENT_SECRET", "").strip(),
        "SSO_REDIRECT_URI": os.getenv("SSO_REDIRECT_URI", "").strip(),
        "SESSION_SECRET": os.getenv("SESSION_SECRET", "").strip(),
    }
    missing = [name for name, value in raw.items() if not value]
    if missing:
        _fatal(f"缺少必需环境变量：{', '.join(missing)}")
        raise SystemExit(1)
    if len(raw["SESSION_SECRET"]) < 32:
        _fatal("SESSION_SECRET 长度必须 ≥ 32 字符")
        raise SystemExit(1)

    port_text = os.getenv("APP_PORT", "").strip() or "8002"
    try:
        port = int(port_text)
    except ValueError:
        _fatal(f"APP_PORT 必须是整数，当前值={port_text}")
        raise SystemExit(1) from None

    return Config(
        issuer=raw["SSO_ISSUER"].rstrip("/"),
        client_id=raw["SSO_CLIENT_ID"],
        client_secret=raw["SSO_CLIENT_SECRET"],
        # 与注册值逐字符一致，不做尾斜杠归一化。
        redirect_uri=raw["SSO_REDIRECT_URI"],
        session_secret=raw["SESSION_SECRET"],
        port=port,
        userinfo_override=os.getenv("SSO_USERINFO_URL_OVERRIDE", "").strip(),
    )


def fetch_discovery(issuer: str) -> dict[str, Any]:
    url = f"{issuer}/.well-known/openid-configuration"
    with httpx.Client(timeout=HTTP_TIMEOUT) as client:
        resp = client.get(url)
        resp.raise_for_status()
        return resp.json()


def fetch_jwks(jwks_uri: str) -> dict[str, Any]:
    with httpx.Client(timeout=HTTP_TIMEOUT) as client:
        resp = client.get(jwks_uri)
        resp.raise_for_status()
        return resp.json()


def ensure_port_free(port: int) -> bool:
    """端口占用直接失败，不自动换端口（换端口会让已注册回调失效）。"""
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        try:
            sock.bind(("0.0.0.0", port))
        except OSError as exc:
            _fatal(f"端口 {port} 不可用（不自动换端口，换端口会让已注册回调失效）：{exc}")
            return False
    return True


# --------------------------------------------------------------------- id_token 验签


class IdTokenVerifier:
    """用 JWK Set 验签 id_token，并校验 iss/aud/exp/nonce。

    kid 轮换后旧 JWKS 会验不过，此时重拉一次并只重试一次——不吞异常。
    """

    def __init__(self, issuer: str, client_id: str, jwks_uri: str, jwks: dict[str, Any]) -> None:
        self._issuer = issuer
        self._client_id = client_id
        self._jwks_uri = jwks_uri
        self._jwks = jwks
        self._jwt = JsonWebToken(["RS256"])

    def verify(self, raw_token: str, nonce: str) -> dict[str, Any]:
        try:
            return self._decode(raw_token, nonce)
        except Exception:  # noqa: BLE001 - 先刷新密钥再判定，最终失败仍向上抛
            self._jwks = fetch_jwks(self._jwks_uri)
            return self._decode(raw_token, nonce)

    def _decode(self, raw_token: str, nonce: str) -> dict[str, Any]:
        claims = self._jwt.decode(
            raw_token,
            self._jwks,
            claims_cls=CodeIDToken,
            claims_options={
                "iss": {"essential": True, "value": self._issuer},
                "aud": {"essential": True, "value": self._client_id},
            },
            # CodeIDToken.validate() 会把 nonce 与这里比对，不一致即抛错。
            claims_params={"nonce": nonce},
        )
        claims.validate()
        return dict(claims)


# ------------------------------------------------------------------------ 令牌交换


def _oauth_client(cfg: Config) -> AsyncOAuth2Client:
    # client_secret_basic = HTTP Basic（契约要求），不用请求体传密钥。
    return AsyncOAuth2Client(
        client_id=cfg.client_id,
        client_secret=cfg.client_secret,
        token_endpoint_auth_method="client_secret_basic",
        timeout=HTTP_TIMEOUT,
    )


async def exchange_code(cfg: Config, token_endpoint: str, code: str, verifier: str) -> dict[str, Any]:
    client = _oauth_client(cfg)
    try:
        token = await client.fetch_token(
            token_endpoint,
            grant_type="authorization_code",
            code=code,
            redirect_uri=cfg.redirect_uri,
            code_verifier=verifier,
        )
        return dict(token)
    finally:
        await client.aclose()


async def refresh_tokens(cfg: Config, token_endpoint: str, refresh_token: str) -> dict[str, Any]:
    client = _oauth_client(cfg)
    try:
        token = await client.refresh_token(token_endpoint, refresh_token=refresh_token)
        return dict(token)
    finally:
        await client.aclose()


async def fetch_userinfo(http: httpx.AsyncClient, url: str, access_token: str) -> dict[str, list[str]]:
    """app_roles / permissions 只在 userinfo 下发（不进 id_token）。"""
    resp = await http.get(url, headers={"Authorization": f"Bearer {access_token}"})
    resp.raise_for_status()
    data = resp.json()
    return {
        "appRoles": _as_str_list(data.get("app_roles")),
        "permissions": _as_str_list(data.get("permissions")),
    }


def _as_str_list(value: Any) -> list[str]:
    if not isinstance(value, list):
        return []
    return [item for item in value if isinstance(item, str)]


def _claim(claims: dict[str, Any], key: str) -> str:
    value = claims.get(key)
    return value if isinstance(value, str) else ""


def _claim_list(claims: dict[str, Any], key: str) -> list[str]:
    return _as_str_list(claims.get(key))


# ---------------------------------------------------------------------------- 应用


@dataclass
class AppState:
    cfg: Config
    discovery: dict[str, Any]
    verifier: IdTokenVerifier
    userinfo_url: str
    store: SessionStore
    serializer: URLSafeTimedSerializer
    http: httpx.AsyncClient
    templates: Jinja2Templates


def _authorize_url(state: AppState, nonce: str, challenge: str, oauth_state: str) -> str:
    # 参数一个都不能少（contracts GET /login）；只允许 S256。
    query = urlencode(
        {
            "response_type": "code",
            "client_id": state.cfg.client_id,
            "redirect_uri": state.cfg.redirect_uri,
            "scope": SCOPES,
            "state": oauth_state,
            "nonce": nonce,
            "code_challenge": challenge,
            "code_challenge_method": "S256",
        }
    )
    return f"{state.discovery['authorization_endpoint']}?{query}"


def _read_sid(request: Request, state: AppState) -> str | None:
    raw = request.cookies.get(COOKIE_NAME)
    if not raw:
        return None
    try:
        sid = state.serializer.loads(raw, max_age=SESSION_TTL)
    except (BadSignature, SignatureExpired):
        return None
    return sid if isinstance(sid, str) and sid else None


def _set_sid(response: Response, state: AppState, sid: str) -> None:
    response.set_cookie(
        COOKIE_NAME,
        state.serializer.dumps(sid),
        max_age=SESSION_TTL,
        path="/",
        httponly=True,
        samesite="lax",
        # 本地 http 演示关闭 Secure；生产必须开启（examples/README.md「仅演示」）。
        secure=False,
    )


def _clear_sid(response: Response) -> None:
    response.delete_cookie(COOKIE_NAME, path="/", httponly=True, samesite="lax")


def _error(status: int, stage: str, code: str, detail: str = "") -> JSONResponse:
    body = {"error": code, "stage": stage, "detail": detail}
    return JSONResponse(body, status_code=status)


def _fmt_time(value: float | None) -> str | None:
    if not value:
        return None
    return datetime.fromtimestamp(value, tz=timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def build_view(
    session: Session,
    userinfo: dict[str, list[str]],
    degraded: list[str],
) -> dict[str, Any]:
    return {
        "sub": session.sub,
        "username": session.username,
        "name": session.name,
        "email": session.email,
        "tenant": session.tenant,
        "deptId": session.dept_id,
        "roles": session.roles,
        "amr": session.amr,
        "appRoles": userinfo["appRoles"],
        "permissions": userinfo["permissions"],
        "degraded": degraded,
        "accessExpiresIn": int(session.access_expires_at - time.time()),
        "lastRefreshedAt": _fmt_time(session.last_refreshed_at),
    }


async def _current(request: Request, state: AppState) -> tuple[Session | None, bool]:
    """返回 (会话, 是否刷新过)；刷新被拒时清掉失效会话，让用户重新授权。"""
    sid = _read_sid(request, state)
    if not sid:
        return None, False

    async def _refresh(session: Session) -> dict[str, Any]:
        started = time.perf_counter()
        try:
            token = await refresh_tokens(state.cfg, state.discovery["token_endpoint"], session.refresh_token)
        except Exception as exc:  # noqa: BLE001 - 统一回落到重新授权
            LOG.info("[refresh] fail reason=%s → reauth", exc)
            raise
        elapsed = int((time.perf_counter() - started) * 1000)
        rotated = 1 if token.get("refresh_token") and token["refresh_token"] != session.refresh_token else 0
        LOG.info("[refresh] ok rotated=%d elapsed=%dms", rotated, elapsed)
        return token

    try:
        session, _refreshed = await state.store.ensure_fresh(sid, _refresh)
    except RefreshRejected:
        state.store.delete(sid)
        return None, False
    return session, True


def _render_index(request: Request, state: AppState, session: Session | None, userinfo, degraded) -> HTMLResponse:
    context: dict[str, Any] = {
        "issuer": state.cfg.issuer,
        "logged_in": session is not None,
        "me": build_view(session, userinfo, degraded) if session else None,
        "has_refreshed": bool(session and session.last_refreshed_at),
    }
    return state.templates.TemplateResponse(request, "index.html", context)


def _lifespan(state: AppState):
    @asynccontextmanager
    async def _scope(_app: FastAPI):
        try:
            yield
        finally:
            await state.http.aclose()

    return _scope


def create_app(cfg: Config, discovery: dict[str, Any]) -> FastAPI:
    jwks_uri = discovery["jwks_uri"]
    # 仅调试：覆盖 userinfo 地址，用于验证降级；不得成为关闭校验的开关。
    userinfo_url = cfg.userinfo_override or discovery["userinfo_endpoint"]

    state = AppState(
        cfg=cfg,
        discovery=discovery,
        verifier=IdTokenVerifier(cfg.issuer, cfg.client_id, jwks_uri, fetch_jwks(jwks_uri)),
        userinfo_url=userinfo_url,
        store=SessionStore(),
        serializer=URLSafeTimedSerializer(cfg.session_secret, salt="sid_py"),
        http=httpx.AsyncClient(timeout=USERINFO_TIMEOUT),
        templates=Jinja2Templates(directory=str(BASE_DIR / "templates")),
    )

    app = FastAPI(
        title="omino-sso Python 接入示例",
        docs_url=None,
        redoc_url=None,
        openapi_url=None,
        lifespan=_lifespan(state),
    )

    @app.get("/", response_class=HTMLResponse)
    async def index(request: Request) -> Response:
        session, _ = await _current(request, state)
        if session is None:
            return _render_index(request, state, None, None, [])

        degraded: list[str] = []
        try:
            userinfo = await fetch_userinfo(state.http, state.userinfo_url, session.access_token)
        except Exception:  # noqa: BLE001 - userinfo 抖动只降级，不放大成 5xx
            userinfo = {"appRoles": [], "permissions": []}
            degraded = ["userinfo"]
        return _render_index(request, state, session, userinfo, degraded)

    @app.get("/login")
    async def login(request: Request) -> Response:
        sid = _read_sid(request, state)
        issued = sid is None
        if sid is None:
            sid = secrets.token_urlsafe(32)

        oauth_state = secrets.token_urlsafe(32)
        nonce = secrets.token_urlsafe(32)
        verifier = secrets.token_urlsafe(48)
        challenge = base64.urlsafe_b64encode(hashlib.sha256(verifier.encode("ascii")).digest()).decode("ascii").rstrip("=")

        state.store.put_pending(sid, Pending(state=oauth_state, nonce=nonce, verifier=verifier, created_at=time.time()))

        response = RedirectResponse(_authorize_url(state, nonce, challenge, oauth_state), status_code=302)
        if issued:
            _set_sid(response, state, sid)
        return response

    @app.get("/callback")
    async def callback(request: Request) -> Response:
        sid = _read_sid(request, state)
        if not sid:
            return _error(400, "callback", "no_pending_authorization", "没有进行中的授权请求")

        pending = state.store.take_pending(sid)
        if pending is None:
            return _error(400, "callback", "no_pending_authorization", "没有进行中的授权请求")

        query = request.query_params
        if query.get("error"):
            return _error(400, "authorize", query.get("error", "authorize_error"), query.get("error_description", ""))
        if query.get("state") != pending.state:
            return _error(400, "callback", "state_mismatch", "state 与本地记录不一致")
        code = query.get("code")
        if not code:
            return _error(400, "callback", "missing_code", "回调缺少 code")

        started = time.perf_counter()
        try:
            token = await exchange_code(cfg, state.discovery["token_endpoint"], code, pending.verifier)
        except Exception as exc:  # noqa: BLE001 - IdP 侧错误透出为 502
            return _error(502, "token_exchange", _oauth_error(exc, "token_exchange_error"), str(exc))

        raw_id_token = token.get("id_token") or ""
        if not raw_id_token:
            return _error(502, "token_exchange", "missing_id_token", "令牌响应没有 id_token")

        try:
            claims = state.verifier.verify(raw_id_token, pending.nonce)
        except Exception as exc:  # noqa: BLE001
            return _error(502, "id_token", "invalid_id_token", str(exc))

        session = Session(
            sub=_claim(claims, "sub"),
            username=_claim(claims, "preferred_username"),
            name=_claim(claims, "name"),
            email=_claim(claims, "email"),
            tenant=_claim(claims, "tenant"),
            dept_id=_claim(claims, "dept_id"),
            roles=_claim_list(claims, "roles"),
            amr=_claim_list(claims, "amr"),
            access_token=token.get("access_token", ""),
            refresh_token=token.get("refresh_token", ""),
            id_token=raw_id_token,
            access_expires_at=_expires_at(token),
        )
        state.store.put(sid, session)

        elapsed = int((time.perf_counter() - started) * 1000)
        LOG.info("[login] sub=%s amr=%s elapsed=%dms", session.sub, session.amr, elapsed)
        return RedirectResponse("/", status_code=302)

    @app.get("/api/me")
    async def me(request: Request) -> Response:
        sid = _read_sid(request, state)
        session, _ = await _current(request, state)
        if session is None:
            response = _error(401, "session", "unauthenticated", "未登录或会话已失效")
            if sid:
                _clear_sid(response)
            return response

        degraded: list[str] = []
        try:
            userinfo = await fetch_userinfo(state.http, state.userinfo_url, session.access_token)
        except Exception:  # noqa: BLE001
            userinfo = {"appRoles": [], "permissions": []}
            degraded = ["userinfo"]
        return JSONResponse(build_view(session, userinfo, degraded))

    @app.get("/logout")
    async def logout(request: Request) -> Response:
        sid = _read_sid(request, state)
        if sid:
            state.store.delete(sid)
        # 不调 end_session：它不清 IdP 的 sid（指南 §8），统一用 /logout?next=
        response = RedirectResponse(f"{cfg.issuer}/logout?next=/logged-out", status_code=302)
        _clear_sid(response)
        return response

    return app


def _expires_at(token: dict[str, Any]) -> float:
    expires_at = token.get("expires_at")
    if isinstance(expires_at, (int, float)) and expires_at > 0:
        return float(expires_at)
    expires_in = token.get("expires_in")
    if isinstance(expires_in, (int, float)) and expires_in > 0:
        return time.time() + float(expires_in)
    return time.time()


def _oauth_error(exc: Exception, fallback: str) -> str:
    code = getattr(exc, "error", None)
    return code if isinstance(code, str) and code else fallback


def main() -> int:
    logging.basicConfig(level=logging.INFO, format="%(message)s", stream=sys.stderr)
    # httpx 的 INFO 日志会为每次出站请求各打一行，容易淹没契约要求的三类日志行。
    logging.getLogger("httpx").setLevel(logging.WARNING)
    cfg = load_config()

    try:
        discovery = fetch_discovery(cfg.issuer)
    except Exception as exc:  # noqa: BLE001 - 启动期把网络错误收敛成一句可读输出
        _fatal(f"连不上 issuer {cfg.issuer}：{exc}")
        return 1

    if not ensure_port_free(cfg.port):
        return 1

    app = create_app(cfg, discovery)

    LOG.info("[start] listen=:%d issuer=%s redirect=%s", cfg.port, cfg.issuer, cfg.redirect_uri)
    if cfg.userinfo_override:
        LOG.info("[start] debug=userinfo_override url=%s", cfg.userinfo_override)

    uvicorn.run(app, host="0.0.0.0", port=cfg.port, log_level="info", access_log=False)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
