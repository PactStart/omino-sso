package com.example.ssoexample;

import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import jakarta.servlet.http.HttpSession;
import java.io.IOException;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.security.core.Authentication;
import org.springframework.security.oauth2.core.oidc.user.OidcUser;
import org.springframework.security.web.authentication.AuthenticationSuccessHandler;
import org.springframework.stereotype.Component;

/** 授权成功：打印契约要求的三类日志行之一，然后回到演示首页。 */
@Component
public class LoginSuccessHandler implements AuthenticationSuccessHandler {

    private static final Logger log = LoggerFactory.getLogger("examples.java");

    @Override
    public void onAuthenticationSuccess(
            HttpServletRequest request, HttpServletResponse response, Authentication authentication)
            throws IOException {

        long elapsedMs = 0L;
        HttpSession session = request.getSession(false);
        if (session != null) {
            Object startedAt = session.getAttribute(SessionKeys.AUTH_STARTED_AT);
            if (startedAt instanceof Long value) {
                elapsedMs = System.currentTimeMillis() - value;
            }
            session.removeAttribute(SessionKeys.AUTH_STARTED_AT);
        }

        if (authentication.getPrincipal() instanceof OidcUser user) {
            log.info(
                    "[login] sub={} amr={} elapsed={}ms",
                    user.getClaims().get("sub"),
                    user.getClaims().get("amr"),
                    elapsedMs);
        }
        response.sendRedirect("/");
    }
}
