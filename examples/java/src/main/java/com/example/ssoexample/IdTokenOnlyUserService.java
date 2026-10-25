package com.example.ssoexample;

import java.util.Collections;
import org.springframework.security.oauth2.client.oidc.userinfo.OidcUserRequest;
import org.springframework.security.oauth2.client.oidc.userinfo.OidcUserService;
import org.springframework.security.oauth2.client.userinfo.OAuth2UserService;
import org.springframework.security.oauth2.core.oidc.user.OidcUserAuthority;
import org.springframework.security.oauth2.core.oidc.user.DefaultOidcUser;
import org.springframework.security.oauth2.core.oidc.user.OidcUser;

/**
 * 只用 id_token 建立登录态，登录阶段不调用 userinfo。
 *
 * <p>Spring 默认的 {@link OidcUserService} 会顺带去 userinfo 拉一次；那样 IdP 的 userinfo
 * 抖动会直接让整条登录失败，与契约「userinfo 不可用只降级、不放大成错误」冲突。
 * 应用内角色由 {@link MeService} 在每次请求里单独取，失败即 degraded。
 */
public class IdTokenOnlyUserService implements OAuth2UserService<OidcUserRequest, OidcUser> {

    @Override
    public OidcUser loadUser(OidcUserRequest userRequest) {
        return new DefaultOidcUser(
                Collections.singleton(new OidcUserAuthority(userRequest.getIdToken())),
                userRequest.getIdToken(),
                "sub");
    }
}
