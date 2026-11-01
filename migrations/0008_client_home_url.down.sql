-- 回滚 011-client-home-url：丢弃首页地址列，首页地址退回「首个回调地址 origin」的推导值。
ALTER TABLE clients DROP COLUMN home_url;
