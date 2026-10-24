package com.example.ssoexample;

import com.fasterxml.jackson.databind.ObjectMapper;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import java.io.IOException;
import java.util.LinkedHashMap;
import java.util.Map;
import org.springframework.security.core.AuthenticationException;
import org.springframework.security.oauth2.core.OAuth2AuthenticationException;
import org.springframework.security.web.authentication.AuthenticationFailureHandler;
import org.springframework.stereotype.Component;

/**
 * 回调失败的统一出口：按契约返回 JSON（error / stage / detail），且不含令牌、密钥、堆栈。
 *
 * <p>IdP 的 OAuth2 错误（含 state 不匹配）记 400；连不上或其它异常记 502。
 */
@Component
public class OAuthFailureHandler implements AuthenticationFailureHandler {

    private final ObjectMapper mapper;

    public OAuthFailureHandler(ObjectMapper mapper) {
        this.mapper = mapper;
    }

    @Override
    public void onAuthenticationFailure(
            HttpServletRequest request, HttpServletResponse response, AuthenticationException exception)
            throws IOException {

        int status = 400;
        String stage = "authorize";
        String code = "authorize_error";
        String detail = String.valueOf(exception.getMessage());

        if (exception instanceof OAuth2AuthenticationException oauth) {
            code = oauth.getError().getErrorCode();
            if ("authorization_request_not_found".equals(code)) {
                // 契约：没有进行中的授权请求时 error 必须是 no_pending_authorization。
                code = "no_pending_authorization";
                stage = "callback";
                detail = "没有进行中的授权请求";
            } else if ("invalid_state_parameter".equals(code)) {
                code = "state_mismatch";
                stage = "callback";
                detail = "state 与本地记录不一致";
            } else if (oauth.getError().getDescription() != null) {
                detail = oauth.getError().getDescription();
            }
        } else {
            status = 502;
            stage = "token_exchange";
        }

        Map<String, Object> body = new LinkedHashMap<>();
        body.put("error", code);
        body.put("stage", stage);
        body.put("detail", detail);

        response.setStatus(status);
        response.setContentType("application/json;charset=UTF-8");
        mapper.writeValue(response.getWriter(), body);
    }
}
