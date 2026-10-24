package com.example.ssoexample;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;

/** omino-sso 的 Java 接入示例：授权码 + PKCE，令牌只存在服务端 HttpSession。 */
@SpringBootApplication
public class DemoApplication {

    public static void main(String[] args) {
        try {
            SpringApplication.run(DemoApplication.class, args);
        } catch (RuntimeException ex) {
            // Spring 的发现失败会打成一大段堆栈；这里补一句契约要求的可读结论（仍然非零退出）。
            if (mentionsIssuer(ex)) {
                System.err.println("[fatal] 连不上 issuer " + System.getenv().getOrDefault("SSO_ISSUER", ""));
            }
            System.exit(1);
        }
    }

    private static boolean mentionsIssuer(Throwable error) {
        for (Throwable cause = error; cause != null; cause = cause.getCause()) {
            String message = cause.getMessage();
            if (message != null && (message.contains("issuer") || message.contains("openid-configuration"))) {
                return true;
            }
        }
        return false;
    }
}
