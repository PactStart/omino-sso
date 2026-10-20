-- omino-sso initial schema
-- 规范：AGENTS.md §6
-- 时间字段统一为 BIGINT（Unix 毫秒）；敏感列使用 utf8mb4_0900_as_cs 或 BINARY(32)

SET NAMES utf8mb4;

CREATE TABLE IF NOT EXISTS users (
    id                  CHAR(26)     NOT NULL,
    username            VARCHAR(64)  CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_as_cs NOT NULL,
    display_name        VARCHAR(64)  NOT NULL,
    email               VARCHAR(128) NULL,
    phone               VARCHAR(20)  NULL,
    avatar_url          VARCHAR(512) NULL,
    password_hash       VARCHAR(255) NOT NULL,
    dept_id             CHAR(26)     NOT NULL,
    tenant_id           VARCHAR(32)  NOT NULL DEFAULT 'default',
    roles               JSON         NOT NULL,
    status              TINYINT      NOT NULL DEFAULT 1,
    mfa_enabled         TINYINT(1)   NOT NULL DEFAULT 0,
    mfa_required        TINYINT(1)   NOT NULL DEFAULT 0,
    mfa_secret          VARBINARY(255) NULL,
    failed_attempts     INT          NOT NULL DEFAULT 0,
    locked_until        BIGINT       NULL,
    last_login_at       BIGINT       NULL,
    password_updated_at BIGINT       NOT NULL,
    created_at          BIGINT       NOT NULL,
    updated_at          BIGINT       NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_users_username (username),
    UNIQUE KEY uk_users_email (email),
    KEY idx_users_dept (dept_id),
    KEY idx_users_tenant (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS organizations (
    id         CHAR(26)     NOT NULL,
    parent_id  CHAR(26)     NULL,
    name       VARCHAR(64)  NOT NULL,
    path       VARCHAR(512) NOT NULL,
    sort       INT          NOT NULL DEFAULT 0,
    tenant_id  VARCHAR(32)  NOT NULL DEFAULT 'default',
    created_at BIGINT       NOT NULL,
    PRIMARY KEY (id),
    KEY idx_orgs_parent (parent_id),
    KEY idx_orgs_path (path)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS clients (
    client_id          VARCHAR(64)  CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_as_cs NOT NULL,
    client_name        VARCHAR(128) NOT NULL,
    client_secret_hash BINARY(32)   NULL,
    is_public          TINYINT(1)   NOT NULL DEFAULT 0,
    redirect_uris      JSON         NOT NULL,
    post_logout_uris   JSON         NOT NULL,
    grant_types        JSON         NOT NULL,
    scopes             JSON         NOT NULL,
    access_token_ttl   INT          NOT NULL DEFAULT 900,
    refresh_token_ttl  INT          NOT NULL DEFAULT 2592000,
    tenant_id          VARCHAR(32)  NOT NULL DEFAULT 'default',
    status             TINYINT      NOT NULL DEFAULT 1,
    created_at         BIGINT       NOT NULL,
    updated_at         BIGINT       NOT NULL,
    PRIMARY KEY (client_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS auth_codes (
    code_hash             BINARY(32)   NOT NULL,
    client_id             VARCHAR(64)  NOT NULL,
    user_id               CHAR(26)     NOT NULL,
    redirect_uri          VARCHAR(512) NOT NULL,
    scope                 VARCHAR(512) NOT NULL,
    nonce                 VARCHAR(128) NULL,
    code_challenge        VARCHAR(128) NOT NULL,
    code_challenge_method VARCHAR(8)   NOT NULL,
    auth_time             BIGINT       NOT NULL,
    amr                   JSON         NOT NULL,
    expires_at            BIGINT       NOT NULL,
    used                  TINYINT(1)   NOT NULL DEFAULT 0,
    created_at            BIGINT       NOT NULL,
    PRIMARY KEY (code_hash),
    KEY idx_codes_client (client_id),
    KEY idx_codes_expires (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS refresh_tokens (
    id           CHAR(26)     NOT NULL,
    token_hash   BINARY(32)   NOT NULL,
    family_id    CHAR(26)     NOT NULL,
    user_id      CHAR(26)     NOT NULL,
    client_id    VARCHAR(64)  NOT NULL,
    scope        VARCHAR(512) NOT NULL,
    amr          JSON         NOT NULL,
    auth_time    BIGINT       NOT NULL,
    expires_at   BIGINT       NOT NULL,
    revoked      TINYINT(1)   NOT NULL DEFAULT 0,
    revoked_at   BIGINT       NULL,
    rotated_from CHAR(26)     NULL,
    created_at   BIGINT       NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_rt_hash (token_hash),
    KEY idx_rt_family (family_id),
    KEY idx_rt_user (user_id),
    KEY idx_rt_expires (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS sessions (
    sid          VARCHAR(64)  NOT NULL,
    user_id      CHAR(26)     NOT NULL,
    tenant_id    VARCHAR(32)  NOT NULL,
    amr          JSON         NOT NULL,
    auth_time    BIGINT       NOT NULL,
    ip           VARCHAR(45)  NOT NULL,
    user_agent   VARCHAR(512) NULL,
    created_at   BIGINT       NOT NULL,
    last_seen_at BIGINT       NOT NULL,
    expires_at   BIGINT       NOT NULL,
    revoked      TINYINT(1)   NOT NULL DEFAULT 0,
    PRIMARY KEY (sid),
    KEY idx_sess_user (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS audit_events (
    id         CHAR(26)     NOT NULL,
    event_type VARCHAR(64)  NOT NULL,
    user_id    CHAR(26)     NULL,
    username   VARCHAR(64)  NULL,
    client_id  VARCHAR(64)  NULL,
    ip         VARCHAR(45)  NOT NULL,
    user_agent VARCHAR(512) NULL,
    result     VARCHAR(16)  NOT NULL,
    detail     JSON         NULL,
    trace_id   VARCHAR(32)  NULL,
    created_at BIGINT       NOT NULL,
    PRIMARY KEY (id),
    KEY idx_audit_type (event_type),
    KEY idx_audit_user (user_id),
    KEY idx_audit_client (client_id),
    KEY idx_audit_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS consents (
    user_id    CHAR(26)    NOT NULL,
    client_id  VARCHAR(64) NOT NULL,
    scopes     JSON        NOT NULL,
    created_at BIGINT      NOT NULL,
    updated_at BIGINT      NOT NULL,
    PRIMARY KEY (user_id, client_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
