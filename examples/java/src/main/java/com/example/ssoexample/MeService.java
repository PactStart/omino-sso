package com.example.ssoexample;

import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import jakarta.servlet.http.HttpSession;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.time.Instant;
import java.time.format.DateTimeFormatter;
import java.util.ArrayList;
import java.util.Base64;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.core.ParameterizedTypeReference;
import org.springframework.http.HttpHeaders;
import org.springframework.http.MediaType;
import org.springframework.http.client.SimpleClientHttpRequestFactory;
import org.springframework.security.core.Authentication;
import org.springframework.security.oauth2.client.OAuth2AuthorizedClient;
import org.springframework.security.oauth2.client.web.OAuth2AuthorizedClientRepository;
import org.springframework.security.oauth2.client.registration.ClientRegistration;
import org.springframework.security.oauth2.core.OAuth2AccessToken;
import org.springframework.security.oauth2.core.OAuth2RefreshToken;
import org.springframework.security.oauth2.core.oidc.user.OidcUser;
import org.springframework.stereotype.Service;
import org.springframework.util.LinkedMultiValueMap;
import org.springframework.util.MultiValueMap;
import org.springframework.web.client.RestClient;

/**
 * 当前用户视图：加载服务端令牌、单飞刷新、取 userinfo。
 *
 * <p>令牌只在服务端 HttpSession 内，页面与 /api/me 都不返回令牌本身。
 */
@Service
public class MeService {

    private static final Logger log = LoggerFactory.getLogger("examples.java");
    private static final Duration REFRESH_SKEW = Duration.ofSeconds(30);
    private static final String GRANT_REFRESH = "refresh_token";

    private final OAuth2AuthorizedClientRepository authorizedClients;
    private final RestClient http;
    private final String userinfoOverride;

    /** 刷新单飞：IdP 的 refresh 是轮换 + 重放即吊销整族，并发刷新会把用户踢下线。 */
    private final Object refreshLock = new Object();

    public MeService(
            OAuth2AuthorizedClientRepository authorizedClients,
            @Value("${app.userinfo-url-override:}") String userinfoOverride) {
        this.authorizedClients = authorizedClients;
        this.userinfoOverride = userinfoOverride == null ? "" : userinfoOverride.trim();

        SimpleClientHttpRequestFactory factory = new SimpleClientHttpRequestFactory();
        factory.setConnectTimeout(5_000);
        factory.setReadTimeout(5_000);
        this.http = RestClient.builder().requestFactory(factory).build();
    }

    /**
     * 返回页面/接口共用的用户视图；未登录或刷新被拒时返回 {@code null}。
     */
    public Map<String, Object> currentView(
            Authentication authentication, HttpServletRequest request, HttpServletResponse response) {

        OAuth2AuthorizedClient client = currentClient(authentication, request, response);
        if (client == null) {
            return null;
        }

        List<String> degraded = new ArrayList<>();
        Map<String, List<String>> userinfo;
        try {
            userinfo = fetchUserInfo(client);
        } catch (Exception ex) {
            // userinfo 抖动不是错误：降级为 200 + degraded=["userinfo"]（SC-009）。
            userinfo = Map.of("appRoles", List.of(), "permissions", List.of());
            degraded.add("userinfo");
        }
        return buildView(authentication, client, userinfo, degraded, request);
    }

    private Map<String, Object> buildView(
            Authentication authentication,
            OAuth2AuthorizedClient client,
            Map<String, List<String>> userinfo,
            List<String> degraded,
            HttpServletRequest request) {

        Map<String, Object> claims = claims(authentication);

        Instant expiresAt = client.getAccessToken().getExpiresAt();
        long expiresIn = expiresAt == null ? 0 : Duration.between(Instant.now(), expiresAt).getSeconds();

        Map<String, Object> view = new LinkedHashMap<>();
        view.put("sub", str(claims, "sub"));
        view.put("username", str(claims, "preferred_username"));
        view.put("name", str(claims, "name"));
        view.put("email", str(claims, "email"));
        view.put("tenant", str(claims, "tenant"));
        view.put("deptId", str(claims, "dept_id"));
        view.put("roles", strList(claims.get("roles")));
        view.put("amr", strList(claims.get("amr")));
        view.put("appRoles", userinfo.get("appRoles"));
        view.put("permissions", userinfo.get("permissions"));
        view.put("degraded", degraded);
        view.put("accessExpiresIn", (int) expiresIn);
        view.put("lastRefreshedAt", lastRefreshedAt(request));
        return view;
    }

    private static Map<String, Object> claims(Authentication authentication) {
        if (authentication != null && authentication.getPrincipal() instanceof OidcUser user) {
            return user.getClaims();
        }
        return Map.of();
    }

    private static String lastRefreshedAt(HttpServletRequest request) {
        HttpSession session = request.getSession(false);
        if (session == null) {
            return null;
        }
        Object value = session.getAttribute(SessionKeys.REFRESHED_AT);
        if (value instanceof Instant instant) {
            return DateTimeFormatter.ISO_INSTANT.format(instant);
        }
        return null;
    }

    // ------------------------------------------------------------------ 令牌

    private OAuth2AuthorizedClient currentClient(
            Authentication authentication, HttpServletRequest request, HttpServletResponse response) {

        OAuth2AuthorizedClient client =
                authorizedClients.loadAuthorizedClient(SecurityConfig.REGISTRATION_ID, authentication, request);
        if (client == null) {
            return null;
        }
        if (isFresh(client)) {
            return client;
        }

        synchronized (refreshLock) {
            // 锁内复查：并发请求里第一个已经换过，其余直接复用新令牌，不再拿旧 refresh_token 去撞轮换。
            OAuth2AuthorizedClient current =
                    authorizedClients.loadAuthorizedClient(SecurityConfig.REGISTRATION_ID, authentication, request);
            if (current == null) {
                return null;
            }
            if (isFresh(current)) {
                return current;
            }
            try {
                return refresh(current, authentication, request, response);
            } catch (Exception ex) {
                // 刷新被拒：清掉失效令牌并回落重新授权，不反复用旧 refresh_token。
                log.info("[refresh] fail reason={} → reauth", ex.getMessage());
                authorizedClients.removeAuthorizedClient(SecurityConfig.REGISTRATION_ID, authentication, request, response);
                return null;
            }
        }
    }

    private static boolean isFresh(OAuth2AuthorizedClient client) {
        Instant expiresAt = client.getAccessToken().getExpiresAt();
        return expiresAt != null && expiresAt.isAfter(Instant.now().plus(REFRESH_SKEW));
    }

    private OAuth2AuthorizedClient refresh(
            OAuth2AuthorizedClient client,
            Authentication authentication,
            HttpServletRequest request,
            HttpServletResponse response) {

        ClientRegistration registration = client.getClientRegistration();
        String currentRefreshToken =
                client.getRefreshToken() == null ? null : client.getRefreshToken().getTokenValue();
        if (currentRefreshToken == null || currentRefreshToken.isEmpty()) {
            throw new IllegalStateException("no refresh_token");
        }

        long startedAt = System.currentTimeMillis();
        MultiValueMap<String, String> form = new LinkedMultiValueMap<>();
        form.add("grant_type", GRANT_REFRESH);
        form.add("refresh_token", currentRefreshToken);

        Map<String, Object> token = http.post()
                .uri(registration.getProviderDetails().getTokenUri())
                .header(HttpHeaders.AUTHORIZATION, basicAuth(registration.getClientId(), registration.getClientSecret()))
                .contentType(MediaType.APPLICATION_FORM_URLENCODED)
                .body(form)
                .retrieve()
                .body(new ParameterizedTypeReference<Map<String, Object>>() {});

        if (token == null || token.get("access_token") == null) {
            throw new IllegalStateException("token response has no access_token");
        }

        Instant issuedAt = Instant.now();
        long expiresIn = token.get("expires_in") instanceof Number number ? number.longValue() : 0L;
        OAuth2AccessToken accessToken = new OAuth2AccessToken(
                OAuth2AccessToken.TokenType.BEARER,
                (String) token.get("access_token"),
                issuedAt,
                issuedAt.plusSeconds(Math.max(expiresIn, 0)),
                client.getAccessToken().getScopes());

        Object rotatedToken = token.get("refresh_token");
        boolean rotated = rotatedToken instanceof String value && !value.equals(currentRefreshToken);
        String refreshValue = rotated ? (String) rotatedToken : currentRefreshToken;
        OAuth2RefreshToken refreshToken = new OAuth2RefreshToken(refreshValue, issuedAt);

        OAuth2AuthorizedClient updated = new OAuth2AuthorizedClient(
                registration, authentication.getName(), accessToken, refreshToken);
        authorizedClients.saveAuthorizedClient(updated, authentication, request, response);
        request.getSession(true).setAttribute(SessionKeys.REFRESHED_AT, issuedAt);

        log.info("[refresh] ok rotated={} elapsed={}ms", rotated ? 1 : 0, System.currentTimeMillis() - startedAt);
        return updated;
    }

    private static String basicAuth(String clientId, String clientSecret) {
        // RFC 6749 §2.3.1 要求先做 form-urlencoded，但本项目的 clientId 是 ULID 后 16 位、
        // secret 是 base64url，字符集本身已在安全范围内。
        String raw = clientId + ":" + clientSecret;
        return "Basic " + Base64.getEncoder().encodeToString(raw.getBytes(StandardCharsets.UTF_8));
    }

    // -------------------------------------------------------------- userinfo

    private Map<String, List<String>> fetchUserInfo(OAuth2AuthorizedClient client) {
        String url = userinfoOverride.isEmpty()
                ? client.getClientRegistration().getProviderDetails().getUserInfoEndpoint().getUri()
                : userinfoOverride;

        Map<String, Object> body = http.get()
                .uri(url)
                .header(HttpHeaders.AUTHORIZATION, "Bearer " + client.getAccessToken().getTokenValue())
                .retrieve()
                .body(new ParameterizedTypeReference<Map<String, Object>>() {});

        Map<String, Object> safe = body == null ? Map.of() : body;
        Map<String, List<String>> out = new LinkedHashMap<>();
        out.put("appRoles", strList(safe.get("app_roles")));
        out.put("permissions", strList(safe.get("permissions")));
        return out;
    }

    // ------------------------------------------------------------------ 工具

    private static String str(Map<String, Object> claims, String key) {
        Object value = claims.get(key);
        return value instanceof String text ? text : "";
    }

    private static List<String> strList(Object value) {
        if (!(value instanceof List<?> raw)) {
            return List.of();
        }
        List<String> out = new ArrayList<>(raw.size());
        for (Object item : raw) {
            if (item instanceof String text) {
                out.add(text);
            }
        }
        return out;
    }
}
