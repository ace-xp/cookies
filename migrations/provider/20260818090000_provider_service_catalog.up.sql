-- 外部服务管理页需要三样已有 schema 没有的东西。

-- 1. 火山语音（TTS）此前从未走过 provider_connections，现在要能存进来。
ALTER TABLE provider_connections
  DROP CHECK chk_provider_connection_type,
  ADD CONSTRAINT chk_provider_connection_type
    CHECK (connection_type IN ('adapter_gateway', 'ark', 'minimax_speech', 'las_operator', 'volcengine_speech'));

-- 2. 已有的 last_verification_ok 是布尔，装不下「密钥无效 / 连不上 / 被拒绝」
--    这三种失败的区别，而页面要按这个区别给出不同的下一步动作。
--    last_verified_at 和 last_verification_message 复用已有列，不重复建。
ALTER TABLE provider_connections
  ADD COLUMN last_verification_outcome VARCHAR(32) NULL AFTER last_verification_ok;

-- 3. 设计文档 4.6 要求审计「谁在什么时候改了哪个服务」。改动本身已经按版本
--    追加在 revisions 表里，缺的只是「谁」。留空串而不是 NULL，避免读取端
--    到处判空；历史行改不出操作人，就是空串。
ALTER TABLE provider_connection_revisions
  ADD COLUMN created_by VARCHAR(128) NOT NULL DEFAULT '' AFTER config_json;
