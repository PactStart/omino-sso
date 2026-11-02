-- 011-client-home-url：应用首页地址落库（门户「我的应用」入口 / 详情页可编辑）。
-- 约定（AGENTS.md §6）：时间列一律 BIGINT（Unix 毫秒）；不建物理外键。
--
-- 在此之前「首页地址」是由首个回调地址的 origin 现场推导的，属于只读展示值：
--   - 注册应用时无法指定，编辑时无法修改；
--   - 回调地址多个时，入口跟着数组顺序漂移。
-- 本次改为独立列，注册与编辑均必填。
ALTER TABLE clients
    ADD COLUMN home_url VARCHAR(512) NULL
    COMMENT '应用首页地址（门户「我的应用」入口）；注册/编辑必填' AFTER logo_url;

-- 存量行回填：取 redirect_uris 首个元素的 origin（scheme://host[:port]），
-- 与改造前 ListForUser 的推导口径完全一致，避免上线瞬间「我的应用」入口失效。
UPDATE clients c
    JOIN (
        SELECT client_id,
               JSON_UNQUOTE(JSON_EXTRACT(redirect_uris, '$[0]')) AS u
        FROM clients
    ) t ON t.client_id = c.client_id
SET c.home_url = CONCAT(
        SUBSTRING_INDEX(t.u, '://', 1),
        '://',
        SUBSTRING_INDEX(SUBSTRING_INDEX(t.u, '://', -1), '/', 1)
    )
WHERE c.home_url IS NULL
  AND t.u IS NOT NULL
  AND t.u LIKE '%://%'
  AND SUBSTRING_INDEX(t.u, '://', 1) <> '';
