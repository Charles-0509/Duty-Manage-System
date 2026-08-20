# Cloudflare Tunnel 与边缘限流

适用域名：

- 开发：`dev.zfye.site`
- 生产：`dms.zfye.site`、`duty.zfye.site`

本方案不启用 Cloudflare Access 或 MFA，也不改变应用的 `0.0.0.0` 监听和局域网访问方式。Cloudflare 只负责公网入口和登录相关接口的第一层限流；应用内限流继续保护 Tunnel 与局域网直连。

## 1. 确认 Tunnel 路由

在 Cloudflare Dashboard 打开 **Zero Trust → Networks → Tunnels**，分别检查开发机和生产机的 Tunnel：

1. Tunnel 状态应为 `Healthy`。
2. 打开 Tunnel 的 **Public Hostnames**。
3. 开发 Tunnel 应包含 `dev.zfye.site`。
4. 生产 Tunnel 应包含 `dms.zfye.site` 和 `duty.zfye.site`。
5. Service Type 选择 `HTTP`，URL 填 DMS 的实际端口，例如当前生产环境使用 `localhost:3000`。不要仅根据旧配置假设端口是 `3100`。

`cloudflared` 与 DMS 位于同一台机器时，Tunnel 应连接回环地址。DMS 自身仍可监听 `0.0.0.0:<APP_PORT>`，所以 Cloudflare 故障时仍能从可信局域网直接排查。

若 `cloudflared` 不在应用服务器上，当前应用的可信代理设置不适用；应先把代理迁回同机，或在代码中只加入该代理主机的准确 IP，不能信任整个私网。

分别检查：

```bash
curl -i https://dev.zfye.site/health
curl -i https://dms.zfye.site/health
curl -i https://duty.zfye.site/health
```

正常响应应为 HTTP 200 和 `{"message":"ok"}`。

再检查响应确实经过 Cloudflare：

```bash
curl -sI https://dms.zfye.site/ | grep -Ei '^(server|cf-ray):'
```

应看到 `server: cloudflare` 和 `cf-ray`。如果返回源站 Nginx/Apache 且没有 `cf-ray`，说明 DNS 仍在绕过 Cloudflare，下面的 WAF 规则不会生效。此时应在 **Zero Trust → Networks → Tunnels → Public Hostnames** 配置域名，并删除与 Tunnel 主机名冲突的直连 A/AAAA 记录；如果有意保留外部反向代理，则至少要在 DNS 页面把对应记录切换为 Proxied（橙色云），再重新验证响应头。

## 2. 创建登录限流规则

进入 `zfye.site` Zone，打开 **Security → WAF → Rate limiting rules**，选择 **Create rule**。
规则名建议：`DMS login rate limit`
自定义表达式：

```text
(http.host in {"dev.zfye.site" "dms.zfye.site" "duty.zfye.site"}
 and http.request.method eq "POST"
 and http.request.uri.path eq "/api/auth/login")
```

参数：

- 计数特征：IP 地址
- 阈值：10 次
- 周期：1 分钟
- 动作：Block
- 缓解超时：10 分钟
- 响应：使用 Cloudflare 默认阻断页或默认 JSON 响应即可

如果当前套餐不能选择精确周期或超时，选择最接近且不更宽松的可用值，并记录实际参数。

## 3. 创建刷新令牌限流规则

规则名建议：`DMS refresh rate limit`。

自定义表达式：

```text
(http.host in {"dev.zfye.site" "dms.zfye.site" "duty.zfye.site"}
 and http.request.method eq "POST"
 and http.request.uri.path eq "/api/auth/refresh")
```

参数：

- 计数特征：IP 地址
- 阈值：60 次
- 周期：1 分钟
- 动作：Block
- 缓解超时：1 分钟

刷新接口的正常频率远低于此阈值；该规则主要拦截异常循环和令牌撞库请求。

## 4. 推荐发布顺序

为降低误封生产用户的风险，先只在表达式中保留 `dev.zfye.site`：

```text
http.host eq "dev.zfye.site"
```

验证开发环境后，再把两条规则的主机条件改成三个域名集合并发布。三个域名属于同一个 `zfye.site` Zone，不需要为每个域名复制规则。

## 5. 验证

1. 正常登录一次，确认页面、刷新令牌轮换和登出正常。
2. 在开发域名上连续提交错误密码，超过阈值后应收到 Cloudflare 的 `429` 或阻断响应。
3. 等待缓解超时结束，再确认正常登录恢复。
4. 在 Cloudflare Dashboard 打开 **Security → Events**，过滤 Hostname 和 URI Path，确认动作来自预期规则。
5. 使用局域网地址访问 `http://<服务器局域网地址>:<APP_PORT>/health`，确认绕过 Tunnel 的故障排查路径仍然可用。
6. 将表达式扩展到两个生产域名后，各验证一次正常登录与刷新，不要在生产环境主动触发大量错误登录。

## 6. 注意事项

- 不要把 `/health`、静态资源或整个 `/api` 放进这两条严格规则，否则容易误伤正常页面加载。
- 不要在 Tunnel 配置或文档中保存、粘贴、提交 Tunnel token。
- Cloudflare 返回的阻断响应不一定是应用 JSON；前端应把它视为普通登录失败并提示稍后重试。
- 局域网请求不会经过 Cloudflare，因此仍由应用自身的登录失败限流保护。
- 应用只信任同机回环代理传入的 `CF-Connecting-IP`；外部客户端不能通过伪造该请求头绕过应用限流。
