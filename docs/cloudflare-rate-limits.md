# Cloudflare Tunnel 边缘限流

以下规则适用于 `dev.zfye.site`、`dms.zfye.site` 和 `duty.zfye.site`。在 Cloudflare Dashboard 的 Security → WAF → Rate limiting rules 中创建；三个域名位于同一区域时可共用规则。

## 登录

表达式：

```text
(http.host in {"dev.zfye.site" "dms.zfye.site" "duty.zfye.site"}
 and http.request.method eq "POST"
 and http.request.uri.path eq "/api/auth/login")
```

- 计数键：IP
- 阈值：1 分钟 10 次
- 动作：Block
- 缓解超时：10 分钟

## 刷新令牌

表达式：

```text
(http.host in {"dev.zfye.site" "dms.zfye.site" "duty.zfye.site"}
 and http.request.method eq "POST"
 and http.request.uri.path eq "/api/auth/refresh")
```

- 计数键：IP
- 阈值：1 分钟 60 次
- 动作：Block
- 缓解超时：1 分钟

发布后分别用三个域名验证正常登录与刷新，再查看 Security Events 确认规则命中。不要为局域网直连改为只监听回环地址；应用内限流同时覆盖 Tunnel 和局域网路径。
