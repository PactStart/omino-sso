-- 006-app-admin-role：应用成员新增「应用管理员」标记。
-- 约定：时间列一律 BIGINT（Unix 毫秒）；client_members 复合主键 (client_id, user_id) 不变。
-- 该标记依附于成员关系：成员被移出即失效，重新加入默认 0（普通成员）。
ALTER TABLE client_members
    ADD COLUMN is_admin TINYINT(1) NOT NULL DEFAULT 0
    COMMENT '1=该应用的应用管理员；授予与取消只在 admin 端' AFTER joined_at;
