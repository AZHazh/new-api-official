# 云导台账号授权

此接入基于 `AZHazh/new-api-official` 的 `5ca405fc0e8c15c0d0a456830e42804f3d45a74a`。云导台复用模型站账号，用户的注册、密码、邮件验证、二次验证、充值和付费继续由模型站管理。云导台只接收经授权确认的账号身份，不管理这些模型站业务。

## 配置

模型站后端进程配置三个环境变量，所有实例保持一致：

```text
DIRECTOR_AUTH_CLIENT_ID=director-web
DIRECTOR_AUTH_CLIENT_SECRET=<至少32字符的随机客户端密钥>
DIRECTOR_AUTH_REDIRECT_URI=http://127.0.0.1:4319/auth/callback
```

本机测试允许 loopback HTTP。正式部署须改为实际云导台 HTTPS 回调，例如 `https://director.example.com/auth/callback`。完整回调精确匹配，不接受通配符、用户凭据、查询参数或 fragment。未配置时新增 Web 授权拒绝服务，原桌面同步不依赖这些配置。

云导台服务端配置同一个客户端密钥环境变量，并指定：

```json
{
  "publicOrigin": "http://127.0.0.1:4319",
  "auth": {
    "type": "tiandou",
    "issuer": "https://www.tiandouai.com",
    "clientId": "director-web",
    "clientSecretEnv": "DIRECTOR_AUTH_CLIENT_SECRET",
    "providerName": "甜豆"
  }
}
```

`issuer` 须为实际模型站 origin；`publicOrigin` 构造出的 `/auth/callback` 须与登记回调一致。以上为身份配置片段，仍需云导台完整的模型、技能和独立项目存储配置。客户端密钥仅用于服务器之间验证身份，不是用户 API 密钥，不能放入前端变量、浏览器存储或版本库。

本仓库只有一套 `web` 前端，不需要切换 classic/default 主题。后端和前端须一同构建部署。

## 授权流程与数据范围

1. 云导台生成与浏览器 HttpOnly Cookie 绑定的 `state` 和 S256 PKCE challenge，跳转模型站 `/desktop-sync`。
2. 未登录时进入模型站登录页；通过同一标签页 sessionStorage 的短期返回记录，在注册、二次验证或 OAuth 登录后继续原授权。返回记录仅接受同源 `/desktop-sync` 的合法 Web 参数，10 分钟过期，不保存密码或令牌。
3. 授权页先向 `GET /api/desktop-sync/web-client` 验证登记的客户端和完整回调，再显示当前账号、返回站点和授权范围。验证未完成时不能同意；非法回调不提供取消跳转。
4. 用户同意后，`POST /api/desktop-sync/issue` 使用模型站现有 `UserAuth()` 验证身份，将账号、客户端、回调和 challenge 绑定至一次性授权码。有效期为 5 分钟。取消则返回原 `state` 和 `error=access_denied`，不签发授权码。
5. 云导台后端通过 `POST /api/desktop-sync/exchange`，使用 HTTP Basic 客户端凭证和 PKCE verifier 交换。模型站再次检查账号存在且启用，只返回账号 ID、用户名、昵称和状态。

Web 授权不返回密码、用户 API 密钥、令牌列表、余额或完整用户对象。云导台按模型站 origin 和稳定用户 ID 确定项目归属；API 密钥轮换不改变身份。

原 Evancod 桌面同步的 localhost `/callback`、令牌列表响应及轮询继续保留。Web 与桌面授权码不能互换用途。

一次性授权码沿用现有存储：单实例可用进程内存，多实例应使用共享 Redis。云导台生成调用所用凭证和扣费策略由云导台服务端另行配置，本授权流程不自动同步用户模型密钥。

## 检查与构建

```sh
go test ./controller -count=1
cd web
bun install --frozen-lockfile
bun test src/features/auth/lib/__tests__/director-authorization.test.ts
bun test src/features/auth/director-authorization/__tests__/consent.test.tsx
bun run typecheck
bun run lint
bun run format:check
bun run build
cd ..
go build -o new-api .
```

上线后从云导台发起登录，检查登录/注册后保留原授权、同意返回显示账号、取消不建立登录、退出后私有接口拒绝访问，以及不同账号项目隔离。邮箱、支付和其他模型站功能继续按模型站自己的部署流程维护，云导台只需验证这些登录路径结束后能够继续原授权。

云导台现有会话没有接收模型站账号禁用的即时通知；模型站禁用会阻止新的授权码交换。已有云导台会话的实时撤销需要后续增加会话复核或通知机制。

## 本次验证记录

2026-11-16 基于本仓库上述提交完成验证：

- 完整控制器回归 `go test ./controller -count=1` 通过，包含客户端凭证、精确回调、S256、重放、过期、禁用账号、身份最小范围及原桌面令牌列表协议。
- 返回路径测试 4 项、授权页交互测试 4 项通过，覆盖等待校验、非法请求禁用、取消返回和签发失败恢复按钮。
- 全项目 TypeScript 检查及本次修改文件的 lint 通过，修改文件的格式检查通过。
- `web` 生产构建及包含前端资源的完整 Go 程序编译通过。
- Chrome 使用完整模型站程序与云导台后端验证密码登录、注册后继续原授权、确认返回账号、退出、取消、非法回调拦截，以及不同账号项目列表和按 ID 访问隔离，浏览器运行错误为 0。

全仓 `bun run lint` 仍报告基线中与本次无关的资源组件、排名、初始化和文档页错误；全仓格式检查仍报告密钥分组单元格及三处视频组件的原有格式差异。这些文件未纳入本授权补丁，不将全仓检查报告为通过。

本机联调使用临时账号、随机测试凭证、隔离 SQLite 和项目目录，没有调用付费模型。临时进程在验证结束后停止。本次代码提交与推送不代表线上部署，真实模型站仍需部署匹配版本并配置双方客户端凭证和回调。
