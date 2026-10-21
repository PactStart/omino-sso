-- 上传文件记录（004-object-storage-upload）
-- 规范：AGENTS.md §6（新表须登记）、specs/004-object-storage-upload/data-model.md
-- 约定：时间列一律 BIGINT（Unix 毫秒）且列名以 _at 结尾；ULID 主键 CHAR(26)。

SET NAMES utf8mb4;

CREATE TABLE IF NOT EXISTS files (
    id         CHAR(26)     NOT NULL,
    provider   VARCHAR(32)  NOT NULL,
    scene      VARCHAR(32)  NOT NULL,
    target_id  VARCHAR(64)  NOT NULL,
    format     VARCHAR(16)  NOT NULL,
    size       BIGINT       NOT NULL,
    url        VARCHAR(512) NOT NULL,
    object_key VARCHAR(512) NOT NULL,
    status     VARCHAR(16)  NOT NULL,
    creator    CHAR(26)     NOT NULL,
    created_at BIGINT       NOT NULL,
    updated_at BIGINT       NOT NULL,
    PRIMARY KEY (id),
    KEY idx_files_scene_created (scene, created_at),
    KEY idx_files_status (status),
    KEY idx_files_target (scene, target_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
