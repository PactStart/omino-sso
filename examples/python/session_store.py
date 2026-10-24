"""进程内会话存储与刷新单飞。

令牌只在这里出现，绝不写进 cookie 或页面（宪法 IV）：浏览器只持有 sid_py。
"""

from __future__ import annotations

import asyncio
import time
from collections.abc import Awaitable, Callable
from dataclasses import dataclass, field
from typing import Any

# 距过期不足 30 秒即视为需要刷新，避免「校验通过后恰好过期」的窗口。
REFRESH_SKEW = 30.0

# pending 只覆盖一次授权往返，超时即作废（防止 state 长期可重放）。
PENDING_TTL = 300.0


class RefreshRejected(Exception):
    """IdP 拒绝了 refresh_token（轮换后重放、过期或整族吊销）。"""


@dataclass
class Pending:
    """只在「授权中」阶段存在，与登录态分开存放，避免互相覆盖。"""

    state: str
    nonce: str
    verifier: str
    created_at: float


@dataclass
class Session:
    """服务端会话：身份字段来自 id_token，三个令牌绝不离开服务端。"""

    sub: str = ""
    username: str = ""
    name: str = ""
    email: str = ""
    tenant: str = ""
    dept_id: str = ""
    roles: list[str] = field(default_factory=list)
    amr: list[str] = field(default_factory=list)

    access_token: str = ""
    refresh_token: str = ""
    id_token: str = ""
    access_expires_at: float = 0.0
    last_refreshed_at: float | None = None
    # 仅用于日志：本次刷新是否发生了 refresh_token 轮换（contracts 的 [refresh] 行）。
    rotated: bool = False

    def needs_refresh(self) -> bool:
        return self.access_expires_at - time.time() <= REFRESH_SKEW


class SessionStore:
    """会话 map + 刷新单飞锁。

    IdP 的 refresh 是轮换 + 重放即吊销整族，并发刷新会把用户踢下线；
    因此刷新必须在锁内做，并在锁内复查「是否已被别人换过」。
    """

    def __init__(self) -> None:
        self._sessions: dict[str, Session] = {}
        self._pendings: dict[str, Pending] = {}
        self._refresh_lock: asyncio.Lock = asyncio.Lock()

    # --- pending -----------------------------------------------------------

    def put_pending(self, sid: str, pending: Pending) -> None:
        # 只保留最新一份：重复点「登录」应覆盖而不是堆叠。
        self._pendings.clear()
        self._pendings[sid] = pending

    def take_pending(self, sid: str) -> Pending | None:
        pending = self._pendings.pop(sid, None)
        if pending is None:
            return None
        if time.time() - pending.created_at > PENDING_TTL:
            return None
        return pending

    # --- session -----------------------------------------------------------

    def put(self, sid: str, session: Session) -> None:
        self._sessions[sid] = session

    def get(self, sid: str) -> Session | None:
        return self._sessions.get(sid)

    def delete(self, sid: str) -> None:
        _ = self._sessions.pop(sid, None)
        _ = self._pendings.pop(sid, None)

    # --- 刷新单飞 ----------------------------------------------------------

    async def ensure_fresh(
        self,
        sid: str,
        refresh: Callable[[Session], Awaitable[dict[str, Any]]],
    ) -> tuple[Session | None, bool]:
        """返回 (会话, 本次是否发生了刷新)；会话不存在返回 (None, False)。

        刷新被拒绝时抛 RefreshRejected，由调用方清掉失效会话，让用户重新授权。
        """
        session = self._sessions.get(sid)
        if session is None:
            return None, False
        if not session.needs_refresh():
            return session, False

        async with self._refresh_lock:
            # 锁内复查：并发请求里第一个已经换过，其余直接复用新令牌。
            current = self._sessions.get(sid)
            if current is None:
                return None, False
            if not current.needs_refresh():
                return current, True

            old_refresh_token = current.refresh_token
            try:
                token = await refresh(current)
            except Exception as exc:  # noqa: BLE001 - 任何刷新失败都回落重新授权
                raise RefreshRejected(str(exc)) from exc

            current.access_token = token.get("access_token", current.access_token)
            new_refresh_token = token.get("refresh_token") or ""
            if new_refresh_token:
                current.refresh_token = new_refresh_token
            current.access_expires_at = _expires_at(token)
            current.last_refreshed_at = time.time()
            current.rotated = bool(new_refresh_token) and new_refresh_token != old_refresh_token
            return current, True


def _expires_at(token: dict[str, Any]) -> float:
    expires_at = token.get("expires_at")
    if isinstance(expires_at, (int, float)) and expires_at > 0:
        return float(expires_at)
    expires_in = token.get("expires_in")
    if isinstance(expires_in, (int, float)) and expires_in > 0:
        return time.time() + float(expires_in)
    # 没有过期信息时保守视为已过期，让下一次访问再走一遍刷新。
    return time.time()
