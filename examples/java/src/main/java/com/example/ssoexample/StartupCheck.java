package com.example.ssoexample;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.boot.ApplicationArguments;
import org.springframework.boot.ApplicationRunner;
import org.springframework.core.env.Environment;
import org.springframework.security.oauth2.client.registration.ClientRegistration;
import org.springframework.security.oauth2.client.registration.ClientRegistrationRepository;
import org.springframework.stereotype.Component;

/**
 * 启动期校验与 [start] 日志。
 *
 * <p>SSO_ISSUER / SSO_CLIENT_ID / SSO_CLIENT_SECRET / SSO_REDIRECT_URI 由 Spring 的占位符解析负责：
 * 缺项会以「Could not resolve placeholder ...」直接启动失败并点名变量。
 * 这里补 SESSION_SECRET 的长度校验（占位符无法表达长度），并打印监听信息。
 */
@Component
public class StartupCheck implements ApplicationRunner {

    private static final Logger log = LoggerFactory.getLogger("examples.java");

    private final String sessionSecret;
    private final String userinfoOverride;
    private final ClientRegistrationRepository registrations;
    private final Environment environment;

    public StartupCheck(
            @Value("${app.session-secret}") String sessionSecret,
            @Value("${app.userinfo-url-override:}") String userinfoOverride,
            ClientRegistrationRepository registrations,
            Environment environment) {
        this.sessionSecret = sessionSecret;
        this.userinfoOverride = userinfoOverride;
        this.registrations = registrations;
        this.environment = environment;
    }

    @Override
    public void run(ApplicationArguments args) {
        if (sessionSecret == null || sessionSecret.trim().length() < 32) {
            // 非零退出：抛异常让 Spring Boot 以失败状态结束进程。
            throw new IllegalStateException("SESSION_SECRET 长度必须 ≥ 32 字符");
        }

        ClientRegistration registration = registrations.findByRegistrationId(SecurityConfig.REGISTRATION_ID);
        log.info(
                "[start] listen=:{} issuer={} redirect={}",
                environment.getProperty("server.port"),
                registration.getProviderDetails().getIssuerUri(),
                registration.getRedirectUri());

        if (userinfoOverride != null && !userinfoOverride.isBlank()) {
            log.info("[start] debug=userinfo_override url={}", userinfoOverride.trim());
        }
    }
}
