package com.example.ssoexample;

import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import jakarta.servlet.http.HttpSession;
import java.time.Duration;
import java.util.LinkedHashMap;
import java.util.Map;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.http.HttpHeaders;
import org.springframework.http.ResponseCookie;
import org.springframework.http.ResponseEntity;
import org.springframework.security.core.Authentication;
import org.springframework.stereotype.Controller;
import org.springframework.ui.Model;
import org.springframework.web.bind.annotation.GetMapping;

/** 契约的四个演示端点 + 首页（不改默认端点，只在其上包一层）。 */
@Controller
public class PageController {

    private final MeService meService;
    private final String issuer;

    public PageController(MeService meService, @Value("${SSO_ISSUER}") String issuer) {
        this.meService = meService;
        this.issuer = issuer.endsWith("/") ? issuer.substring(0, issuer.length() - 1) : issuer;
    }

    /** GET /login：记下发起时间后交给 Spring 的授权端点（它会带上 PKCE 与 state）。 */
    @GetMapping("/login")
    public String login(HttpServletRequest request) {
        request.getSession(true).setAttribute(SessionKeys.AUTH_STARTED_AT, System.currentTimeMillis());
        return "redirect:/oauth2/authorization/" + SecurityConfig.REGISTRATION_ID;
    }

    /**
     * GET /callback 的兜底：只有「没有进行中的授权请求」时才会走到这里。
     *
     * <p>Spring 的 OAuth2LoginAuthenticationFilter 处理成功/失败的回调时会直接写响应，不进入控制器。
     */
    @GetMapping("/callback")
    public ResponseEntity<Map<String, Object>> callbackWithoutPending() {
        return ResponseEntity.badRequest()
                .body(error("no_pending_authorization", "callback", "没有进行中的授权请求"));
    }

    @GetMapping("/")
    public String index(
            Authentication authentication,
            HttpServletRequest request,
            HttpServletResponse response,
            Model model) {

        Map<String, Object> view = meService.currentView(authentication, request, response);
        model.addAttribute("loggedIn", view != null);
        if (view != null) {
            model.addAttribute("me", view);
            model.addAttribute("hasRefreshed", view.get("lastRefreshedAt") != null);
        }
        return "index";
    }

    @GetMapping("/api/me")
    public ResponseEntity<Map<String, Object>> me(
            Authentication authentication, HttpServletRequest request, HttpServletResponse response) {

        Map<String, Object> view = meService.currentView(authentication, request, response);
        if (view == null) {
            return ResponseEntity.status(401).body(error("unauthenticated", "session", "未登录或会话已失效"));
        }
        return ResponseEntity.ok(view);
    }

    /** GET /logout：清服务端会话与 sid_java，再交给 IdP 的 /logout（end_session 不清 sid）。 */
    @GetMapping("/logout")
    public String logout(HttpServletRequest request, HttpServletResponse response) {
        HttpSession session = request.getSession(false);
        if (session != null) {
            session.invalidate();
        }
        ResponseCookie cleared = ResponseCookie.from("sid_java", "")
                .path("/")
                .maxAge(Duration.ZERO)
                .httpOnly(true)
                .sameSite("Lax")
                .build();
        response.addHeader(HttpHeaders.SET_COOKIE, cleared.toString());
        return "redirect:" + issuer + "/logout?next=/logged-out";
    }

    private static Map<String, Object> error(String code, String stage, String detail) {
        Map<String, Object> body = new LinkedHashMap<>();
        body.put("error", code);
        body.put("stage", stage);
        body.put("detail", detail);
        return body;
    }
}
