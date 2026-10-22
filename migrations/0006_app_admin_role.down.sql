-- 回滚 006-app-admin-role：丢弃「应用管理员」标记，成员关系保留。
ALTER TABLE client_members DROP COLUMN is_admin;
