-- 010-app-access-org-grant：应用访问按组织架构授权（门户可见性）。
-- 约定（AGENTS.md §6）：时间列一律 BIGINT（Unix 毫秒）；不建物理外键（逻辑外键）；
-- 不存 org_path 快照 —— 部门迁父时子树 path 会整体平移，快照会指向错误的子树。
CREATE TABLE IF NOT EXISTS client_org_grants (
    client_id   VARCHAR(64) COLLATE utf8mb4_0900_as_cs NOT NULL COMMENT '应用标识，与 clients.client_id 同排序规则',
    org_id      CHAR(26)    NOT NULL COMMENT '授权对象部门（organizations.id）',
    include_sub TINYINT(1)  NOT NULL DEFAULT 1 COMMENT '1=命中该部门整棵子树；0=仅该部门本级',
    granted_by  CHAR(26)    NULL COMMENT '操作者用户 ID；系统/脚本路径为空',
    granted_at  BIGINT      NOT NULL COMMENT '首次授权时间',
    updated_at  BIGINT      NOT NULL COMMENT '最近一次 include_sub 变更时间',
    PRIMARY KEY (client_id, org_id),
    KEY idx_client_org_grants_org (org_id)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4 COMMENT ='应用 × 部门的门户可见性授权（010）';
