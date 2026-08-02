# New-API 数据迁移完整指南

## 概述

本文档指导你从**天豆定制版 tiandou-api** 平滑迁移到**官方升级版 new-api**,保持旧版在线运行,新版在 8080 端口并行测试,验证无误后切换流量。

---

## 一、迁移前准备

### 1. 当前环境确认

你的生产环境:
- **旧版应用**: 容器 `new-api-app`,镜像 `ghcr.io/azhazh/tiandou-api:v1.2.3`
- **数据库**: SQLite,文件路径 `/opt/new-api/data/new-api.db` (54MB,30 张表)
- **核心数据**: 62 用户、14 渠道、82 令牌、4 万+ 日志
- **端口**: 3000 (nginx 反代)
- **定制功能**: 桌面端授权(保留)、结算中心(已废弃,不迁移)

### 2. 服务器要求

- Docker & Docker Compose
- SQLite 3
- 足够磁盘空间 (数据库当前 54MB,建议预留 200MB+ 用于备份)
- Root 或 sudo 权限

---

## 二、代码移植(本地完成)

在本地 `/Users/mr_an/Documents/new-api` 项目中,以下功能已移植完成:

### 后端 (Go)

1. **controller/desktop_sync.go** (245 行)
   - 桌面端/VSCode 授权同步 API
   - 使用 `pkg/cachex.HybridCache` (Redis/内存双模式)
   
2. **router/api-router.go** (新增路由)
   ```go
   desktopRoute := apiRouter.Group("/desktop-sync")
   desktopRoute.Use(middleware.UserAuth())
   {
       desktopRoute.POST("/issue", controller.IssueDesktopAuthCode)
       desktopRoute.POST("/exchange", controller.ExchangeDesktopToken)
       desktopRoute.POST("/sessions", controller.StoreDesktopSession)
       desktopRoute.GET("/sessions/:state", controller.GetDesktopSession)
   }
   ```

### 前端 (React + TypeScript)

1. **web/src/routes/_authenticated/desktop-sync.tsx** (200+ 行)
   - 桌面端授权页面,用户选择令牌授权 Evancod
   
2. **web/src/features/home/components/sections/hero.tsx** (新增下载区)
   - 首页 Hero 区域添加 "下载 Evancod" 按钮
   - 自动检测系统(Mac/Windows)下载对应安装包

### 部署脚本

- **bin/deploy-official-new-api.sh**
  - 基于旧版脚本改造
  - 使用官方镜像 `calciumion/new-api:latest`
  - 部署到 `/opt/new-api-official` + 8080 端口
  - 自动备份、健康检查、回滚支持

---

## 三、服务器操作步骤

### 步骤 1: 上传新代码到服务器

在**本地**打包修改后的代码:

```bash
cd /Users/mr_an/Documents/new-api
tar --exclude='.git' --exclude='web/node_modules' --exclude='web/dist' \
    -czf /tmp/new-api-official.tar.gz \
    controller/desktop_sync.go \
    router/api-router.go \
    bin/deploy-official-new-api.sh \
    Dockerfile \
    main.go \
    go.mod \
    go.sum \
    web/
```

上传到服务器:

```bash
scp /tmp/new-api-official.tar.gz root@你的服务器:/root/
```

### 步骤 2: 服务器上部署新版 (8080 端口)

SSH 到服务器:

```bash
ssh root@你的服务器

# 解压代码
cd /root
tar -xzf new-api-official.tar.gz -C /tmp/new-api-build

# 构建 Docker 镜像 (可选,如果不用官方镜像的话)
# cd /tmp/new-api-build
# docker build -t new-api:custom-latest .

# 首次部署,初始化目录和配置
bash /tmp/new-api-build/bin/deploy-official-new-api.sh latest
```

首次运行会在 `/opt/new-api-official/` 创建目录和 `docker-compose.deploy.yml`,**暂停在这一步**。

### 步骤 3: 复制旧版数据库

```bash
# 停止新版容器 (如果已启动)
cd /opt/new-api-official
docker compose -f docker-compose.deploy.yml -f docker-compose.override.yml down 2>/dev/null || true

# 备份旧版数据库
sqlite3 /opt/new-api/data/new-api.db ".backup '/tmp/new-api-migration.db'"

# 复制到新版目录
cp /tmp/new-api-migration.db /opt/new-api-official/data/new-api.db

# 验证复制完整性
echo "旧版数据库:"
sqlite3 /opt/new-api/data/new-api.db "SELECT count(*) FROM users"
echo "新版数据库:"
sqlite3 /opt/new-api-official/data/new-api.db "SELECT count(*) FROM users"
```

两个数字应该一致 (62 用户)。

### 步骤 4: 启动新版应用

```bash
cd /opt/new-api-official

# 再次运行部署脚本,这次会拉取镜像并启动
bash /tmp/new-api-build/bin/deploy-official-new-api.sh latest
```

脚本会:
- 备份数据库到 `/opt/new-api-official/backups/`
- 拉取 `calciumion/new-api:latest`
- 启动容器 `new-api-official-app` (监听 8080)
- 健康检查 `http://localhost:8080`

**关键**: 首次启动时,官方版会运行 `AutoMigrate`,自动添加新表:
- `user_sessions` (会话管理)
- `auth_flows` (认证流程)
- `casbin_rules` (权限控制)
- `authz_roles` (角色管理)
- 等其他 8 张新表

旧表会**原地升级**,添加新字段,**数据不会丢失**。

### 步骤 5: 验证新版功能

#### 5.1 基础功能

```bash
# 检查容器状态
docker ps | grep new-api-official

# 查看启动日志 (确认 AutoMigrate 成功)
docker logs new-api-official-app | grep -i "migrat\|error" | tail -30
```

#### 5.2 Web 功能测试

在浏览器访问 `http://你的服务器IP:8080`:

1. **登录**: 用旧版账号密码登录 (root / 你的密码)
2. **渠道列表**: 检查 14 个渠道是否正常显示
3. **令牌列表**: 检查 82 个令牌
4. **桌面端授权**: 访问 `http://你的服务器IP:8080/desktop-sync?state=test&redirect_uri=http://example.com`,看授权页面是否正常
5. **首页下载按钮**: 访问首页,检查 "下载 Evancod" 按钮是否显示

#### 5.3 API 功能测试

```bash
# 测试旧令牌在新版上是否可用
export OLD_TOKEN="sk-xxxxx"  # 从旧版复制一个有效令牌

curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $OLD_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [{"role":"user","content":"test"}],
    "max_tokens": 10
  }'
```

应该返回正常响应或明确的错误 (非 500)。

### 步骤 6: Nginx 切换流量

确认新版功能正常后,修改 nginx 配置,将 3000 端口流量切到 8080。

#### 6.1 找到 nginx 配置文件

```bash
# 查找包含 new-api 或 3000 的配置
grep -r "3000\|new-api" /etc/nginx/sites-enabled/ /etc/nginx/conf.d/ 2>/dev/null
```

#### 6.2 修改 upstream

假设配置类似:

```nginx
upstream new_api {
    server 127.0.0.1:3000;  # 旧版
}

server {
    listen 80;
    server_name api.你的域名.com;
    
    location / {
        proxy_pass http://new_api;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

改为:

```nginx
upstream new_api {
    server 127.0.0.1:8080;  # 新版
    # server 127.0.0.1:3000;  # 旧版 (注释掉,保留以便回滚)
}
```

重载 nginx:

```bash
nginx -t  # 测试配置
nginx -s reload
```

#### 6.3 验证切换

```bash
# 从外部访问 (通过域名)
curl -I https://api.你的域名.com

# 应该看到新版的响应头或首页
```

---

## 四、回滚预案

如果新版出现问题,立即回滚:

### 方法 A: Nginx 切回旧版 (最快)

```bash
# 修改 nginx upstream
vim /etc/nginx/.../your-config

# 改回:
# server 127.0.0.1:3000;  # 旧版
# # server 127.0.0.1:8080;  # 新版

nginx -s reload
```

旧版容器一直在运行,秒级恢复。

### 方法 B: 恢复旧版数据库 (数据回滚)

如果新版写坏了数据:

```bash
# 停止新版
cd /opt/new-api-official
docker compose -f docker-compose.deploy.yml down

# 从备份恢复
LATEST_BACKUP=$(ls -t /opt/new-api-official/backups/ | head -1)
cp /opt/new-api-official/backups/$LATEST_BACKUP/new-api.db /opt/new-api/data/new-api.db

# 重启旧版
docker restart new-api-app
```

---

## 五、迁移后清理 (可选)

新版运行稳定 1-2 周后:

```bash
# 停止旧版
docker stop new-api-app
docker rm new-api-app

# 备份旧版数据后删除
tar -czf /root/old-new-api-backup-$(date +%F).tar.gz /opt/new-api
rm -rf /opt/new-api

# 修改 nginx upstream 删除旧版注释
# 释放 3000 端口供其他服务使用
```

---

## 六、结算中心表处理

旧版数据库中的 4 张结算中心表会保留但不再使用:
- `settlement_sites`
- `settlement_leases`
- `settlement_events`
- `settlement_audit_logs`

如果确认不需要历史数据,可以删除:

```bash
sqlite3 /opt/new-api-official/data/new-api.db <<SQL
DROP TABLE IF EXISTS settlement_sites;
DROP TABLE IF EXISTS settlement_leases;
DROP TABLE IF EXISTS settlement_events;
DROP TABLE IF EXISTS settlement_audit_logs;
SQL
```

**建议**: 先运行 1 个月,确认无影响后再删除。

---

## 七、常见问题

### Q1: AutoMigrate 失败怎么办?

查看日志:

```bash
docker logs new-api-official-app | grep -i "error\|fail\|migrat"
```

常见原因:
- SQLite 文件损坏 → 从备份恢复
- 权限问题 → `chown -R root:root /opt/new-api-official/data`

### Q2: 旧令牌在新版上无法使用?

检查令牌表是否完整:

```bash
sqlite3 /opt/new-api-official/data/new-api.db "SELECT id, name, status FROM tokens LIMIT 5"
```

如果数据丢失,说明数据库复制有问题,重新执行步骤 3。

### Q3: 桌面端授权页面 404?

检查路由是否生效:

```bash
docker logs new-api-official-app | grep "desktop-sync"
```

应该看到路由注册日志。如果没有,说明后端代码未正确编译进镜像,需要重新构建。

### Q4: 如何从 SQLite 迁移到 PostgreSQL?

新版运行稳定后,想换数据库:

1. 导出 SQLite 数据 (使用 `.dump` 或第三方工具如 `pgloader`)
2. 修改 `docker-compose.deploy.yml`,添加 postgres 服务
3. 设置 `SQL_DSN` 环境变量
4. 导入数据到 postgres
5. 重启应用

**注意**: SQLite → Postgres 迁移比较复杂,建议先在测试环境验证。

---

## 八、联系支持

- 官方文档: https://docs.newapi.pro
- GitHub Issues: https://github.com/Calcium-Ion/new-api/issues
- 迁移问题请附上:
  - 旧版版本号 (v1.2.3)
  - 新版版本号 (latest 对应的具体版本)
  - 错误日志 (脱敏后的前 50 行)
  - 数据库表结构 (`sqlite3 xxx.db ".schema" > schema.txt`)
