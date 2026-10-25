package com.example.ssoexample;

/** 会话属性名集中一处，避免控制器与服务层各写一遍字符串。 */
final class SessionKeys {

    /** /login 跳转授权端点的时间戳（毫秒），用于打印 [login] elapsed。 */
    static final String AUTH_STARTED_AT = "omino.authStartedAt";

    /** 上次成功刷新的时间（Instant），页面「会话状态」行用。 */
    static final String REFRESHED_AT = "omino.refreshedAt";

    private SessionKeys() {
    }
}
