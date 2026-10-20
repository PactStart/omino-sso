-- 为"我的应用"补充展示字段（AGENTS.md §6.3）
ALTER TABLE clients
    ADD COLUMN description VARCHAR(255) NULL AFTER client_name,
    ADD COLUMN logo_url    VARCHAR(512) NULL AFTER description;
