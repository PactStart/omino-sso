-- 回退 005-verification-code-auth
SET NAMES utf8mb4;

ALTER TABLE users DROP KEY uk_users_phone;

DROP TABLE IF EXISTS send_records;
