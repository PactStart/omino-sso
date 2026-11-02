-- 回滚 010-app-access-org-grant：丢弃全部组织授权配置。
-- 执行后全部组织授权不可恢复，回滚需先回滚服务镜像再执行本文件（critique E13）；
-- 手动成员、角色绑定与审计事件不受影响。
DROP TABLE IF EXISTS client_org_grants;
