-- 应用内成员 / 角色 / 权限 / 授权 与 access token 上下文映射
-- 规范：AGENTS.md §6（新表须登记）、specs/003-app-management-pages/data-model.md
-- 约定：时间列一律 BIGINT（Unix 毫秒）且列名以 _at 结尾；
--       client_id 与角色名/权限编码必须大小写敏感（utf8mb4_0900_as_cs）。

SET NAMES utf8mb4;

-- 应用成员：门户可见性的唯一依据（FR-012）。
CREATE TABLE IF NOT EXISTS client_members (
    client_id VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_as_cs NOT NULL,
    user_id   CHAR(26)    NOT NULL,
    added_by  CHAR(26)    NULL,
    joined_at BIGINT      NOT NULL,
    PRIMARY KEY (client_id, user_id),
    KEY idx_client_members_user (user_id, client_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- 应用内角色：名称在应用内唯一且大小写敏感（FR-016）。
CREATE TABLE IF NOT EXISTS client_roles (
    id          CHAR(26)     NOT NULL,
    client_id   VARCHAR(64)  CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_as_cs NOT NULL,
    name        VARCHAR(32)  CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_as_cs NOT NULL,
    description VARCHAR(100) NULL,
    created_at  BIGINT       NOT NULL,
    updated_at  BIGINT       NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_client_roles_name (client_id, name),
    KEY idx_client_roles_client (client_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- 应用内权限点：编码在应用内唯一且大小写敏感（FR-020）；父子层级由 parent_id 表达。
CREATE TABLE IF NOT EXISTS client_permissions (
    id          CHAR(26)     NOT NULL,
    client_id   VARCHAR(64)  CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_as_cs NOT NULL,
    parent_id   CHAR(26)     NULL,
    code        VARCHAR(64)  CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_as_cs NOT NULL,
    name        VARCHAR(64)  NOT NULL,
    description VARCHAR(255) NULL,
    sort        INT          NOT NULL DEFAULT 0,
    created_at  BIGINT       NOT NULL,
    updated_at  BIGINT       NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_client_permissions_code (client_id, code),
    KEY idx_client_permissions_parent (client_id, parent_id, sort)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- 角色↔权限绑定。
CREATE TABLE IF NOT EXISTS client_role_permissions (
    role_id       CHAR(26)    NOT NULL,
    permission_id CHAR(26)    NOT NULL,
    client_id     VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_as_cs NOT NULL,
    created_at    BIGINT      NOT NULL,
    PRIMARY KEY (role_id, permission_id),
    KEY idx_role_permissions_perm (permission_id),
    KEY idx_role_permissions_client (client_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- 角色↔成员绑定（INV-7：绑定的成员必然同时是 client_members 的成员）。
CREATE TABLE IF NOT EXISTS client_role_members (
    role_id    CHAR(26)    NOT NULL,
    user_id    CHAR(26)    NOT NULL,
    client_id  VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_as_cs NOT NULL,
    created_at BIGINT      NOT NULL,
    PRIMARY KEY (role_id, user_id),
    KEY idx_role_members_user (user_id),
    KEY idx_role_members_client (client_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- access token → 应用 的上下文映射（research R2）：userinfo 按令牌所属应用计算角色与权限。
CREATE TABLE IF NOT EXISTS access_tokens (
    token_id   VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_as_cs NOT NULL,
    sub        CHAR(26)     NOT NULL,
    client_id  VARCHAR(64)  CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_as_cs NOT NULL,
    expires_at BIGINT       NOT NULL,
    created_at BIGINT       NOT NULL,
    PRIMARY KEY (token_id),
    KEY idx_access_tokens_exp (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
