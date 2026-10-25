package com.example.ssoexample;

import org.springframework.security.oauth2.client.registration.ClientRegistrationRepository;
import org.springframework.security.oauth2.client.web.DefaultOAuth2AuthorizationRequestResolver;
import org.springframework.security.oauth2.client.web.OAuth2AuthorizationRequestCustomizers;
import org.springframework.security.oauth2.client.web.OAuth2AuthorizationRequestResolver;
import org.springframework.security.oauth2.client.web.OAuth2AuthorizedClientRepository;
import org.springframework.security.oauth2.client.web.HttpSessionOAuth2AuthorizedClientRepository;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.context.annotation.Primary;
import org.springframework.security.config.annotation.web.builders.HttpSecurity;
import org.springframework.security.config.annotation.web.configuration.EnableWebSecurity;
import org.springframework.security.config.annotation.web.configurers.AbstractHttpConfigurer;
import org.springframework.security.web.SecurityFilterChain;

/**
 * 安全配置：授权码 + PKCE（S256），会话 cookie 名由 application.yaml 的 sid_java 决定。
 *
 * <p>四个演示端点全部 permitAll：登录态由「有没有服务端 authorized client」判断，
 * 而不是靠框架的 302 拦截（契约要求 /api/me 未登录返回 401 JSON，而不是跳到登录页）。
 */
@Configuration
@EnableWebSecurity
public class SecurityConfig {

    /** IdP 的注册 id，与 application.yaml 的 registration 键一致。 */
    static final String REGISTRATION_ID = "omino";

    @Bean
    SecurityFilterChain filterChain(
            HttpSecurity http,
            ClientRegistrationRepository registrations,
            LoginSuccessHandler successHandler,
            OAuthFailureHandler failureHandler) throws Exception {

        // PKCE：Spring 只对「无密钥的公共客户端」自动启用，机密客户端要显式加 S256。
        OAuth2AuthorizationRequestResolver resolver =
                new DefaultOAuth2AuthorizationRequestResolver(registrations, "/oauth2/authorization");
        ((DefaultOAuth2AuthorizationRequestResolver) resolver)
                .setAuthorizationRequestCustomizer(OAuth2AuthorizationRequestCustomizers.withPkce());

        http
                .authorizeHttpRequests(auth -> auth.anyRequest().permitAll())
                // 演示只有 GET；生产必须开启 CSRF 并把登出改成 POST（examples/README.md「仅演示」）。
                .csrf(AbstractHttpConfigurer::disable)
                // Spring 的 LogoutFilter 只认 POST，且会吞掉 GET /logout；登出由控制器处理。
                .logout(AbstractHttpConfigurer::disable)
                .oauth2Login(oauth -> oauth
                        // 关掉自动生成的登录页，/login 由控制器直接跳授权端点。
                        .loginPage("/login")
                        .authorizationEndpoint(endpoint -> endpoint.authorizationRequestResolver(resolver))
                        // 默认是 /login/oauth2/code/{id}，必须与 redirect-uri 的路径同时对齐到 /callback。
                        .redirectionEndpoint(endpoint -> endpoint.baseUri("/callback"))
                        // 不用 OidcUserService 的默认实现：它会在登录时调用 userinfo，
                        // 一旦 userinfo 不可用整条登录就失败，而契约要求降级而不是失败。
                        .userInfoEndpoint(endpoint -> endpoint.oidcUserService(new IdTokenOnlyUserService()))
                        .successHandler(successHandler)
                        .failureHandler(failureHandler));
        return http.build();
    }

    /** 默认实现把令牌放在服务端 HttpSession（浏览器只拿 sid_java），正是契约束形。 */
    @Bean
    @Primary
    OAuth2AuthorizedClientRepository authorizedClientRepository() {
        return new HttpSessionOAuth2AuthorizedClientRepository();
    }
}
