-- 多通道验证与账号安全（005-verification-code-auth）
-- 规范：AGENTS.md §6、specs/005-verification-code-auth/data-model.md
-- 约定：时间列一律 BIGINT（Unix 毫秒）且列名以 _at 结尾；ULID 主键 CHAR(26)。

SET NAMES utf8mb4;

-- 短信 / 邮箱验证码发送记录（审计型流水，不使用软删除，按保留期物理清理）。
-- target 为脱敏后的接收方（如 138****8000、z***@example.com），不可反查真实联系方式。
CREATE TABLE IF NOT EXISTS send_records (
    id         CHAR(26)     NOT NULL,
    channel    VARCHAR(16)  NOT NULL,
    scene      VARCHAR(32)  NOT NULL,
    target     VARCHAR(128) NOT NULL,
    result     VARCHAR(16)  NOT NULL,
    provider   VARCHAR(32)  NOT NULL,
    error      VARCHAR(255) NULL,
    created_at BIGINT       NOT NULL,
    PRIMARY KEY (id),
    KEY idx_send_records_created (created_at),
    KEY idx_send_records_channel_created (channel, created_at),
    KEY idx_send_records_scene_created (scene, created_at),
    KEY idx_send_records_result_created (result, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- users.phone 已存在（0001_init），本期补唯一索引（FR-004）。
-- 前置：若存量数据存在重复手机号（非 NULL），本语句会失败；上线前须先人工核对：
--   SELECT phone, COUNT(*) FROM users WHERE phone IS NOT NULL GROUP BY phone HAVING COUNT(*) > 1;
ALTER TABLE users ADD UNIQUE KEY uk_users_phone (phone);
