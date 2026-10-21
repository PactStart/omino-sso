-- 回滚 0003_app_auth：按依赖逆序 DROP。
-- 警告：执行后成员、角色、权限、授权与 access token 映射配置**不可恢复**。
-- 回滚顺序必须"先回退二进制、观察 ≥1 个发布周期、再执行本脚本"
-- （见 specs/003-app-management-pages/quickstart.md §10.2）。

SET NAMES utf8mb4;

DROP TABLE IF EXISTS access_tokens;
DROP TABLE IF EXISTS client_role_members;
DROP TABLE IF EXISTS client_role_permissions;
DROP TABLE IF EXISTS client_permissions;
DROP TABLE IF EXISTS client_roles;
DROP TABLE IF EXISTS client_members;
