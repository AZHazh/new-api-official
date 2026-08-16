# api.seedance.nz 视频能力完整接入计划

## 1. 文档目的

本文给出 `api.seedance.nz` 视频能力接入当前项目的完整实施方案。方案基于以下信息完成核查：

- 仓库根目录的 `video.md`
- `https://api.seedance.nz/docs/#models`
- `https://api.seedance.nz/docs/llms.txt`
- 当前项目的视频路由、异步任务、渠道分发、计费、轮询、下载代理和前端渠道配置

本文最初用于指导实施。截至 2026 年 8 月 15 日，主体代码已按本方案完成改造；后续保留原计划正文，实际完成情况和未验证边界以紧随其后的“实施状态”为准。

## 实施状态（截至 2026 年 8 月 15 日）

当前状态是“主体功能和自动化验证已完成，上线前的真实上游与多数据库环境验收尚未完成”。首期仍严格遵守既定范围：不接 Seedance 钱包、不转存视频、不支持多 Key，也不开放非视频能力。

### 已实现

- 新增独立的 Seedance 渠道类型、任务适配器和本地能力注册表，覆盖 105 个主视频/视频处理模型、3 个 Context IR 模型和 1 个 Midjourney Video 模型；已知非视频模型被隐藏，未知模型按 `unknown` 失败关闭。
- 接通 `/v1/videos`、Context IR 旧协议、Midjourney Video、素材上传、本地任务查询和后台上游轮询；包含退避、超时恢复、状态单调更新及用户查询不直连上游。
- 每个下游模型使用本站独立的正数 `ModelPrice`。普通视频和 Context IR 按固定最大费用预扣，Midjourney Video 按 `ModelPrice × batch_size` 预扣；未定价或余额不足时不会请求上游。
- 持久化任务计费状态和幂等计费事件，覆盖预扣、成功结算、明确失败、提交结果不确定、超时与强制渠道操作后的全额退款，并支持进程中断后的恢复。
- 上游任务 ID、签名 URL 和工作流 artifact 均保存在私有数据中；下游仅使用本地任务 ID 和本站代理 URL。视频代理支持 Range、批量结果、附属资源、过期处理和下载响应。
- 素材上传已实现 50 MB、类型校验、流式转发及按上游 Key 聚合的跨实例限流；渠道管理实现单 Key 校验、只读连通性测试、模型分类发现，以及存在运行任务时的默认保护和管理员显式强制退款路径。
- 前端已补充 Seedance 渠道配置、分类模型发现、视频 API 示例、任务状态与下载展示，并完成七种语言文案同步。

### 已完成验证

- 后端全量测试：`go test -count=1 ./...`
- 账务并发、幂等退款和中断恢复路径的定向 `go test -race`
- 独立模块构建：`cd relaykit && GOWORK=off go build ./...`
- 前端 Seedance 定向测试共 7 项通过，并通过 `bun run i18n:sync`、`bun run typecheck`、`bun run build` 及改动文件的定向格式和 lint 检查
- 七个 locale 的缺失、冗余和未翻译计数均为 0；工作树通过 `git diff --check`

### 尚未完成的环境验收

- 未使用真实 Seedance 付费账户提交视频任务，因此尚未对生产上游的实际鉴权、提交、轮询、临时结果 URL、下载和上游余额不足响应做付费端到端 smoke test。
- 尚未在独立的 MySQL 5.7.8+ 和 PostgreSQL 9.6+ 实例执行迁移、行锁、唯一幂等事件和并发 CAS 集成测试；当前不能把自动化通过等同于 SQLite、MySQL、PostgreSQL 三数据库实机验收完成。
- 尚未执行生产规模的多实例长时间轮询、真实大文件下载和上游限流压测。

因此，当前代码已达到进入管理员灰度验收的条件，但不能视为第 20 节“上线验收标准”全部完成。上线前仍应执行第 19 节阶段 6 中的三数据库环境测试和一次管理员明确授权的低价真实视频端到端验收。

## 2. 已确认的业务决策

| 项目 | 决策 |
| --- | --- |
| 接入范围 | 接入 106 个视频输出能力、3 个视频提示词增强能力和视频必需的素材上传能力 |
| 上游钱包 | 不接入 `/api/usage/wallet/`，也不使用钱包余额差推算任务成本 |
| 本站售价 | 由本站独立配置，不跟随上游任务实际扣费 |
| 预扣策略 | 在请求上游前预扣该请求可能产生的最大本站费用，余额不足直接拒绝，不请求上游 |
| 任务查询 | 后台定时轮询上游；用户查询读取本地任务状态，不在每次用户查询时同步访问上游 |
| 视频存储 | 首期不转存，保留上游临时 URL，并通过本站下载代理提供下载 |
| 上游密钥 | 首期单渠道、单 API Key，不设计多 Key 轮换 |
| 模型映射 | 保留现有下游模型别名到上游模型名的映射能力 |

## 3. 接入范围

### 3.1 纳入范围

按当前文档显式列出的 model/action 统计，本次范围是：

- 106 个真正输出视频或执行视频超分的能力，其中 105 个走主视频协议，1 个是 Midjourney Video
- 3 个视频专用提示词增强能力
- 1 个视频素材上传端点

运行时以本地能力注册表为安全真值；上游模型发现只提供管理员候选和可用性提示，不自动赋予路由能力，也不依赖文档标题中的错误总数。

| 能力分组 | 视频输出数量 |
| --- | ---: |
| Seedance 2.0 | 18 |
| Seedance 2.5 Standard | 6 |
| Wan 2.7 Spicy | 1 |
| HappyHorse 1.1 | 3 |
| RunningHub 视频输出 | 71 |
| Zhenzhen Upscaler | 1 |
| Zhenzhen 扩展视频 | 5 |
| Midjourney Video | 1 |
| 合计 | 106 |

3 个 MiniMax H3 Context IR 另计为视频提示词增强，不加入视频输出合计。

#### 主视频协议

- `POST /v1/videos`：提交视频生成或视频处理任务
- `GET /v1/videos/{task_id}`：查询本地视频任务
- `GET /v1/videos/{task_id}/content`：通过本站代理预览或下载结果
- `POST /v1/video/generations`：保留项目已有的旧版下游兼容入口；视频输出兼容请求转为调用上游 `/v1/videos`，3 个 Context IR 模型例外，仍调用上游旧协议 `/v1/video/generations`
- `GET /v1/video/generations/{task_id}`：保留已有兼容查询格式

#### 视频素材上传

- `POST /v1/files/upload`
- 支持文档声明的图片、音频、视频格式
- 图片和音频只作为视频/Context IR 参考素材，不开放对应的图片或音频生成能力
- 上传结果是上游生成的 24 小时临时直链
- 上传本身不计费，但必须鉴权、限流、限制大小并校验文件类型

#### 视频模型家族

- Seedance 2.0 国内版和国际版的 t2v、i2v、multi
- Seedance 2.5 Standard 国内版和国际版的 t2v、i2v、multi
- Wan 2.7 Spicy 视频
- HappyHorse 1.1 视频
- Kling 视频生成、编辑、动作、口型同步等视频输出 SKU
- Hailuo 2.3 和 Hailuo H3 视频
- FLUX 3 Video
- MiniMax H3 OW 视频
- Vidu Q3 视频
- Zhenzhen 扩展视频
- `zhenzhen-upscaler` 视频超分

#### 视频提示词增强

- `minmax-h3-context-ir-text`
- `minmax-h3-context-ir-image`
- `minmax-h3-context-ir-multimodal`
- 这 3 个 SKU 通过旧版 `POST /v1/video/generations` 提交并通过 `GET /v1/video/generations/{task_id}` 查询
- 它们只返回 `result_text`，不计入 106 个视频输出能力，但属于完整视频工作流的辅助能力

#### Midjourney Video

- `POST /v1/midjourney/generations/video`
- `GET /v1/midjourney/tasks/{task_id}`
- 仅接入 Video 动作，不接入 Imagine、Upscale、Variation 等图片动作
- 支持直接传 `image_urls` 和可选 `end_url` 的完整图生视频流程
- 上游的 `task_id + index` 输入模式依赖一个先前的 Midjourney 图片任务。由于本次明确不接图片能力，首期不接受原始上游 task ID，也不复用旧 Midjourney 模块的任务 ID。该模式返回明确的 `400 unsupported_source_task`，避免跨用户引用或泄露上游 ID

#### 管理能力

- Seedance 渠道创建、编辑、禁用和模型映射
- 通过上游 `GET /v1/models` 拉取模型
- 只读渠道连通性测试
- 本站模型定价配置
- 视频任务日志和管理员审计信息

### 3.2 明确排除

- Seedance 上游钱包余额查询
- 图片生成和图片编辑
- 音频生成、语音转写和音乐
- Chat Completions
- Suno
- Midjourney 非 Video 动作
- 视频结果自动转存、长期对象存储和 CDN
- 多 API Key、Key 轮换和跨上游容灾
- 根据上游实际 Token、`thirdPartyConsumeMoney`、`cost` 或钱包余额差结算用户费用

## 4. 上游协议核查结论

### 4.1 主视频链路

```text
POST /v1/videos
  -> 返回上游任务 ID，初始状态 queued
  -> GET /v1/videos/{upstream_task_id}
  -> queued / in_progress / completed / failed
  -> completed 时读取 metadata.url
  -> failed 时读取 error.code 和 error.message
```

关键契约：

- 鉴权为 `Authorization: Bearer <API Key>`
- `seconds` 在主请求中是字符串，不是 JSON 数字
- `prompt` 是否必填由模型类型决定，i2v 可以不传 prompt
- `metadata` 的结构和限制随模型家族变化，不能只做通用透传
- `metadata.url` 是临时签名 URL，文档要求尽快下载
- 上游公开查询响应没有稳定的任务级实际费用字段
- 上游推荐每 3 到 5 秒查询一次
- 上游返回 HTTP `402 payment_required` 时表示 Seedance 渠道账户余额不足，不表示本站用户余额不足
- 提交和查询按完整 2xx 范围判断成功，再校验响应中的任务 ID/状态；非 2xx 先限量读取并结构化解析错误，不能只接受 200 或把整段响应拼进错误日志

### 4.2 素材上传链路

```text
POST /v1/files/upload (multipart/form-data)
  -> 上游保存素材
  -> 返回 url、file_type、size、expires_in
  -> 客户端把 url 放入 images、metadata.content、video_url 或 audio_url
  -> 再提交视频任务
```

文档限制：

- 单文件最大 50 MB
- 上游限制每令牌每分钟 10 次、每天 200 次
- 返回 URL 有效期约 24 小时
- 视频生成模型自身可能有更严格的素材数量、格式和大小限制

### 4.3 视频提示词增强链路

```text
POST /v1/video/generations
  -> model=minmax-h3-context-ir-*
  -> 返回上游任务 ID
  -> GET /v1/video/generations/{upstream_task_id}
  -> SUCCESS / FAILURE
  -> SUCCESS 时读取 result_text
```

该分支继续使用异步 Task，但成功结果是文本而不是媒体 URL。查询转换器必须返回 `result_text`，不能套用普通视频的 `metadata.url`。

### 4.4 Midjourney Video 链路

```text
POST /v1/midjourney/generations/video
  -> 返回 data[0].task_id
  -> GET /v1/midjourney/tasks/{upstream_task_id}
  -> SUCCESS / FAILURE
  -> SUCCESS 时读取 video_url 和 video_urls
```

关键限制：

- 仅 i2v，不支持纯 t2v
- 固定 FAST 模式
- 输出时长约 5 秒
- `batch_size` 只允许 1、2、4
- `video_type` 决定 480p、720p 和起止帧模式
- `batch_size` 是本站计费乘数，必须在预扣前完成上限校验

### 4.5 文档中的不一致

公开文档的部分模型总数与分项数量不一致：

- `RunningHub OpenAPI 媒体模型（共 66 个）` 的显式列表实际是 74 个，其中 71 个输出视频，3 个是 Context IR
- `Zhenzhen 扩展视频 / 图片（共 12 个）` 的显式列表实际是 5 个视频加 8 个图片，共 13 个

因此不能把文档标题中的总数当作代码常量。实现采用两层真值：

1. 上游 `/v1/models` 用于发现当前可用模型。
2. 本地能力注册表决定一个模型是否属于视频、允许哪些参数和如何校验。

上游新出现但本地无法识别能力的模型只能显示为“待适配”，不能自动加入可路由模型，避免非视频模型或未知计费参数被误开放。

## 5. 当前项目链路

### 5.1 可复用部分

当前项目已经具备以下能力：

- `router/video-router.go` 中的 OpenAI Video 提交、查询和内容代理路由
- `controller.RelayTask` 的渠道选择和重试入口
- `relay/channel.TaskAdaptor` 异步任务适配器边界
- 下游公开 task ID 与上游真实 task ID 隔离
- `model.Task` 的任务持久化
- 系统任务调度和后台轮询
- 本站用户额度、订阅和 Token 的预扣能力
- 失败退款和任务差额日志骨架
- `common.QuotaFrom*Checked` 的额度饱和保护和审计
- `controller.VideoProxy` 的用户归属校验和 SSRF 防护

### 5.2 不能直接照搬的部分

#### 不能复用 Sora 适配器

Seedance 虽然同样使用 `/v1/videos`，但以下行为不同：

- 模型数量和模型级参数差异很大
- i2v prompt 可以为空
- 部分模型允许 `seconds=-1`
- Seedance 2.5 的时长和多模态素材上限不同
- 视频超分不使用 prompt 和 seconds
- Midjourney Video 使用另一套提交、查询和响应结构
- 成功结果直接位于 `metadata.url`

因此必须新增独立适配器，不能在 Sora 适配器中累积条件分支。

#### 通用请求校验不适用

现有通用视频校验会强制 prompt，并拒绝某些合法的智能时长。Seedance 必须使用专属、强类型 DTO 和模型能力注册表校验。

项目已有 `POST /v1/videos/{video_id}/remix`，但 Seedance 公开协议没有对应上游端点。该路由选中 Seedance 渠道时必须在本地返回明确的 400 `unsupported_operation`，不能猜测或拼接上游 remix URL；Zhenzhen extend、FLUX draft-enhance 等使用各自已文档化的模型字段完成。

#### 当前任务成功响应写得过早

现有适配器可能在本地 Task 插入前就向客户端写入成功响应。若数据库写入失败，会出现以下状态：

- 上游任务已经运行
- 用户已经扣费
- 客户端拿到了 task ID
- 本地却没有可轮询和退款的任务

Seedance 接入必须改为“本地任务持久化成功后才返回 200”。

#### 当前终态与退款之间存在崩溃窗口

现有轮询先把 Task 改成终态，再执行退款或结算。进程在两步之间退出时，终态任务不会再次进入轮询，可能永久漏退款。完整接入必须加入持久化计费状态和幂等事件。

#### 当前下载代理不完整

现有下载代理没有完整转发 Range，并会在错误日志中记录完整签名 URL。大视频下载、断点续传和签名参数保密都需要补齐。

## 6. 总体架构

### 6.1 隔离原则

新增渠道类型 `ChannelTypeSeedance`。如果编号 61 在实施分支仍未被占用，则使用 61，并把 `ChannelTypeDummy` 后移。实施时必须再次检查编号，不能盲目写死。

核心实现放在独立目录：

```text
relay/channel/task/seedance/
  adaptor.go             主视频 TaskAdaptor
  dto.go                 上游请求和响应 DTO
  catalog.go             视频模型能力注册表
  validate.go            模型级请求校验
  billing.go             本站预扣报价
  response.go            状态和下游响应转换
  context_ir.go          Context IR 旧协议分支
  midjourney_video.go    Midjourney Video 协议分支
  upload.go              素材上传上游客户端
```

该包只依赖现有公共任务接口、HTTP 客户端和 `common.*` JSON 包装，不依赖 Sora、Kling、Vidu 等具体适配器。`catalog.go` 是 106 个视频输出能力和 3 个 Context IR 能力的显式分类真值，提交、模型展示、模型发现过滤、校验和测试都复用这份注册表，避免多处维护不同清单。

### 6.2 允许的共享改动

为了让接入可用且账务可靠，需要少量共享改动：

- 注册新渠道类型和 TaskAdaptor
- 复用现有 `/v1/videos` 和 `/v1/video/generations` 路由，不重复注册；只新增素材上传和 Seedance 版 Midjourney Video 专用路由
- 在 `middleware/distributor.go` 中为 Midjourney Video 专用路由设置固定视频模型、relay mode 和本地任务查询分支，不能落入现有 `/mj/*` 图片任务逻辑
- 增加持久化的任务计费状态和幂等事件
- 将“写客户端响应”延迟到任务落库之后
- 为 Seedance 增加独立轮询调度
- 完善视频下载代理

为实现延迟响应而不重写所有旧适配器，在 `relay/channel/adapter.go` 增加可选的 deferred task response 能力：Seedance `DoResponse` 只解析上游响应并返回上游 ID 与安全 `Task.Data`，不直接写 `gin.Context.Writer`；可选接口再构造下游响应并放入新增的 `TaskSubmitResult.ClientResponse`。`controller.RelayTask` 在任务创建、上游 ID 更新和计费状态持久化全部成功后才写这份响应。查询侧增加可选的任务协议响应转换能力，由 Seedance adaptor 分别生成 OpenAI Video、旧版 Context IR 和 Midjourney Video 格式。

现有 `controller/model.go:init()` 只遍历同步 `Adaptor`，不会自动读取 `TaskAdaptor.GetModelList()`。Seedance 又不能注册普通 Chat APIType，因此必须显式从 Seedance catalog 把 106 个视频输出模型和 3 个 Context IR 模型接入默认模型元数据及 `channelId2Models`，否则渠道虽然能创建，默认模型清单仍会缺失。

这些改动应做成通用任务能力，但首期只让 Seedance 使用新路径，避免一次性重写其他视频供应商。

## 7. 完整请求链路

### 7.1 普通视频提交

```text
客户端
  -> TokenAuth
  -> 读取并限制请求体
  -> Distribute 按本地下游模型选择 Seedance 渠道
  -> Seedance DTO 解析
  -> 应用模型映射
  -> 按上游模型能力做二次校验
  -> 生成本站价格报价和最大预扣额度
  -> 余额不足：直接返回 402，不访问上游
  -> 创建本地 SUBMITTING Task
  -> 幂等预扣
  -> POST Seedance /v1/videos
  -> 保存 upstream_task_id 和 queued 状态
  -> 本地 Task 的上游关联与 billing_status 更新持久化成功
  -> 将本次 BillingSession 固定到冻结的 reserved_quota，并只记录一次提交消费日志/request_count
  -> 返回只含本地 task_id 的成功响应
```

本地 `SUBMITTING` Task 创建失败时不得预扣、不得请求上游。上游已接单但本地关联更新持续失败时不得向客户端返回成功，应进入可审计的 `SUBMIT_UNKNOWN + refund_pending` 恢复路径；若数据库故障连该状态也无法落库，则重试落库后退款，并按“潜在上游孤儿任务”告警，不能静默吞错。

这里的本站余额不足才返回 `402`。如果 Seedance 在提交时返回 `402 payment_required`，含义是上游渠道账户余额不足：本站应把本地任务与 `refund_pending` 一起可靠落库，停止同渠道重试，触发全额幂等退款，并向客户端返回 `502 Bad Gateway`、错误码 `upstream_balance_insufficient`。绝不能把上游 `402` 原样透传成“本站用户余额不足”。

### 7.2 后台轮询

```text
Seedance 轮询任务每 5 秒取得到期任务
  -> 按 channel_id 分组
  -> 按任务协议查询上游：
       普通视频 GET /v1/videos/{upstream_task_id}
       Context IR GET /v1/video/generations/{upstream_task_id}
       Midjourney Video GET /v1/midjourney/tasks/{upstream_task_id}
  -> 临时网络错误、429、5xx：记录错误并退避，不改终态
  -> queued / in_progress：单调更新状态和进度
  -> completed / SUCCESS：保存视频代理地址或 result_text，标记成功并关闭计费状态
  -> failed：进入 refund_pending，执行全额幂等退款
```

用户的普通视频 GET、旧版 Context IR GET 和 Midjourney Video GET 都只查询本地数据库，不直接请求上游。这样可以防止大量用户请求放大成上游轮询流量，也能保证返回的始终是本地公开 ID。

### 7.3 视频下载

```text
客户端 GET /v1/videos/{local_task_id}/content
  -> TokenOrUserAuth
  -> 校验任务属于当前用户
  -> 校验任务成功且结果 URL 未知为过期
  -> SSRF 校验
  -> 流式代理上游临时 URL
  -> 转发 Range / Content-Range / Content-Length / Content-Type
```

首期不把视频写入本站磁盘或对象存储。

用户点击下载时只发生鉴权后的流式代理，不触发本站落盘、对象存储上传或后台转存；存储能力作为后续阶段单独设计。

### 7.4 素材上传

`/v1/files/upload` 没有 model 字段，不能直接使用按模型分发的 `middleware.Distribute()`。首期只有一个 Seedance 上游，因此采用专用选择器：

1. 查找当前用户组可用且启用的 Seedance 渠道，并确认当前 Token 至少允许一个该渠道的已启用视频/Context IR 模型；否则返回 403。
2. 必须恰好选出一个渠道；没有渠道返回 503，配置出多个渠道时返回管理员可识别的配置错误，避免随机上传到不同账户。
3. 以流式 multipart 方式转发文件，不把整个 50 MB 文件读入内存。
4. 不向客户端暴露渠道 Key。

Seedance 渠道保存时校验为单 API Key；若检测到项目通用的多 Key 配置格式，直接拒绝并提示首期不支持。未来需要多 Seedance 渠道时，再为上传增加明确的渠道亲和策略，不在本次提前实现多 Key。

### 7.5 Context IR 提交

```text
客户端 POST /v1/video/generations
  -> model 必须是 3 个 minmax-h3-context-ir-* 之一
  -> 按 catalog 校验 prompt、seconds 和各分支 metadata
  -> 读取该 Context IR 模型的本站 ModelPrice
  -> 请求上游前按固定最大价预扣，余额不足不请求上游
  -> 创建本地 Task 并冻结报价快照
  -> POST Seedance /v1/video/generations
  -> 保存上游任务 ID 到 PrivateData
  -> 本地任务更新成功后返回本地 task_id
  -> 后台轮询旧版查询端点
  -> SUCCESS 保存 result_text；FAILURE、取消或超时进入全额退款
```

该分支不生成视频文件，不提供 `/content` 下载，但属于视频工作流的必要提示词增强能力。不得把它路由到 `/v1/videos`，也不得因为它返回文本而落入 Chat Completions。

### 7.6 视频内的多步依赖

完整视频范围内仍有几类“前一视频任务产物作为下一任务输入”的链路，不能按普通字段裸透传：

- Zhenzhen `extend_from_task_id`：下游只能传本站本地 task ID；校验任务属于当前用户、已成功、同一 Seedance 渠道且模型允许延长后，再转换为私有上游 task ID
- FLUX `draft_cache`：把上游返回的 catalog 声明 continuation artifact 保存为私有原值，对外只返回本站 `artifact_*` 句柄；下一次提交只接受曾由当前用户成功任务产生、类型匹配且未过期的句柄
- Kling lip-sync 的 `sessionId`、`faceId`：identify-face 等前置视频任务产物按 artifact 保存，对外字段值同样替换为本站句柄；后续 tts/video 步骤必须验证当前用户、同渠道和 artifact kind
- `return_last_frame`：若上游返回最后一帧图片 URL，完整签名 URL 存私有结果资产，用户通过本站 `/content?asset=last_frame` 获取

为兼容请求形状，`draft_cache`、`sessionId`、`faceId` 可以保留原字段名，但字段值必须是随机生成且不可枚举的本站 `artifact_*` 句柄，不能把上游原值返回客户端。后续输入先按句柄查询，验证当前用户、来源任务已成功、同一渠道、kind 匹配且未过期，再在构造上游请求的最后一步替换为私有上游原值；任意伪造、跨用户、跨渠道、类型不匹配或过期句柄均返回 400。依赖图片生成任务的 Midjourney `task_id + index` 仍按 3.1 的范围决定排除。

## 8. 请求 DTO 与模型校验

### 8.1 DTO 规则

Seedance 请求会被解析后重新发给上游，因此所有可选标量必须使用指针并带 `omitempty`，例如：

```go
Seconds          *string `json:"seconds,omitempty"`
Seed             *int    `json:"seed,omitempty"`
GenerateAudio    *bool   `json:"generate_audio,omitempty"`
ReturnLastFrame  *bool   `json:"return_last_frame,omitempty"`
BatchSize        *int    `json:"batch_size,omitempty"`
```

这样可以区分“未传”和显式传入 `0`、`false`。所有 JSON 编解码必须使用 `common.Marshal`、`common.Unmarshal`、`common.DecodeJson` 等包装函数。

RunningHub 部分模型的 `metadata` 字段没有完整结构说明。对 catalog 明确允许、但暂时不能稳定建模的字段，使用 `map[string]json.RawMessage` 保存原始 JSON 值，避免数字、布尔值或嵌套对象在中转时被 `map[string]any` 改写。`encoding/json` 只用于引用 `json.RawMessage` 类型，实际 Marshal/Unmarshal 仍全部走 `common.*`。

无损保留不等于任意透传：每个模型仍维护允许字段名白名单；任何会影响时长、数量、分辨率或输出批次的字段必须解析为强类型、做上界校验后再进入计费。白名单外字段直接返回 400。

### 8.2 校验顺序

当前任务接口先校验后做模型映射，这会让模型别名无法按真正的上游模型校验。计划调整为：

1. 解析请求并取得原始模型名。
2. 应用渠道模型映射。
3. 用 `UpstreamModelName` 查能力注册表并校验。
4. 用 `OriginModelName` 查本站售价。
5. 构建上游请求。

为降低对现有适配器的影响，可以增加一个可选的“映射后校验”接口，Seedance 先使用，其他适配器保持原行为。

### 8.3 能力注册表

能力注册表不保存上游价格，只保存协议和安全边界：

```text
model pattern
task kind
prompt min/max/required
image min/max
video min/max
audio min/max
seconds allowed range or enum
whether -1 is allowed
resolution enum
ratio enum
required metadata fields
count upper bounds
polling protocol
```

### 8.4 必须覆盖的模型差异

| 家族 | 关键校验 |
| --- | --- |
| Seedance 2.0 t2v | prompt 必填；无素材；模型级 seconds、tier、resolution 校验；return_last_frame 显式 false 不能丢失 |
| Seedance 2.0 i2v | images 1 到 2；prompt 可选；首尾帧顺序固定 |
| Seedance 2.0 multi | prompt 必填；图片不超过 9、视频不超过 3、音频不超过 3，且至少一个素材 |
| Seedance 2.5 | 4 到 30 秒或模型允许的智能时长；multi 最多 30 图、10 视频、10 音频，总数不超过 50 |
| Wan 2.7 Spicy | i2v；图片必填；2 到 15 秒；720p 或 1080p |
| HappyHorse | 3 到 15 秒；不允许 -1；r2v 图片 1 到 9 |
| Kling | 按 t2v、i2v、r2v、edit、motion、lip-sync 分别检查图片、视频、session 和 face 参数；多步 artifact 必须验证归属 |
| Hailuo | 2.3 与 H3 使用不同的时长、分辨率和素材规则 |
| FLUX 3 Video | t2v、i2v、v2v、draft-enhance 分开校验；5 到 20 秒；safety_tolerance 0 到 4；draft_cache 必须来自当前用户的有效前置任务 |
| MiniMax H3 OW | 时长只允许 5、10、15；分辨率只允许 480p、720p；音频驱动限制一张图和一条音频 |
| MiniMax H3 Context IR | 仅 3 个显式 SKU；prompt 1 到 7000；seconds 4 到 15 的字符串枚举；text/image/multimodal 分别校验 ratio 和素材要求；成功结果是 result_text |
| Vidu Q3 | t2v、i2v、start-end、r2v、short-play 分开校验 |
| Zhenzhen Video | 五个视频 SKU 分别校验固定时长、分辨率、图片数和 video_url；extend_from_task_id 只接受可转换的本地任务 ID |
| Zhenzhen Upscaler | 恰好一个 MP4 video_url；目标分辨率枚举；prompt 和 seconds 不参与上游请求 |
| Midjourney Video | 一张起始图；可选结束图；batch_size 仅 1、2、4；video_type 白名单 |

### 8.5 防止校验绕过

- 同一参数无论出现在顶层还是 `metadata` 都必须归一化后校验
- `metadata.content` 不能绕过图片、视频、音频数量上限
- 任何 `n`、`batch_size`、duration、seconds 都必须先做上界检查再进入计费
- 所有 duration/seconds 除模型级边界外还必须受 `relaycommon.MaxTaskDurationSeconds` 总上限保护；与既有图片输出数量同义的 `n` 复用 `dto.MaxImageN`
- 使用 `*uint` 等无符号字段时仍必须检查上界，不能用“非负”代替范围校验
- 计费乘数只能通过 `types.PriceData.AddOtherRatio` 写入，不能直接修改 `OtherRatios`
- catalog 白名单内的 opaque 字段可用 `json.RawMessage` 无损保留；不认识的字段和计费乘数字段默认拒绝，不允许直接透传
- 对 URL 只接受 `http` 和 `https`
- 上游素材 URL 的可访问性由上游最终校验，本站不为普通提交主动下载任意外部素材

## 9. 本站独立计费方案

### 9.1 计费原则

本站用户费用只由本站配置决定。上游文档中的 Token 单价、`thirdPartyConsumeMoney`、`cost` 和 Seedance 钱包余额不参与用户结算。

首期 106 个视频输出能力和 3 个 Context IR 中，每个启用的下游模型都必须配置本站 `ModelPrice`。Seedance 渠道不允许回退到 `ModelRatio / 2` 的占位预扣；价格缺失时在访问上游前返回模型未定价错误。这里仍使用项目现有的本站用户额度、订阅和 Token 计费基础设施，但不查询 Seedance 的 `/api/usage/wallet/`。

### 9.2 最大费用预扣

定义：

```text
request_max_price = 本站固定模型价格 × 所有已验证的本站计费乘数
reserved_quota = request_max_price × QuotaPerUnit × 提交时分组倍率
```

转换必须使用 `common.QuotaFromDecimalChecked` 或对应 Checked 帮助函数，禁止裸 `int(...)` 转换。发生饱和时把 `QuotaClamp` 保存到 relay/task 计费上下文，并在提交或退款日志写入 `other.admin_info.quota_saturation` 和请求关联告警；饱和后的超大预扣只能因额度不足被拒绝，不能溢出成负扣费。

首期规则：

- 普通单结果视频：预扣等于该模型配置的固定按次价格
- Context IR：按对应 SKU 的固定最大按次价格预扣，成功不按返回文本长度重算
- Midjourney Video：固定单段价格乘 `batch_size`，`batch_size` 在计费前限制为 1、2、4
- 其他出现输出数量的模型：只有能力注册表明确允许并配置乘数时才接受
- `seconds=-1`：按该模型配置的最大按次价格收费，不等待上游实际时长调整
- Zhenzhen Upscaler：首期按固定最大按次价格收费，不根据不可信的客户端时长结算
- 分组倍率在提交时冻结，任务运行期间改价不影响已有任务

如果以后需要按时长或分辨率差异化售价，可以新增本站 `VideoPriceRules`，但必须满足：

- 每个维度是有限枚举
- 有明确最大值
- 报价在请求上游前完成
- 智能时长和无法验证的媒体时长使用最大档
- 最终成功费用不得超过预扣

首期不把异步视频接入 token 型 `billingexpr`。当前表达式体系是按 Token 变量设计，并且任务链路尚未完整接入该快照，强行复用会扩大耦合和结算风险。

### 9.3 完整资金流

#### 本地校验失败

- 不创建上游任务
- 不扣费
- 返回 400

#### 余额不足

- 按最大费用检查本站用户额度、订阅和 Token 额度
- 任一必需额度不足，返回 402
- 不请求上游

#### 上游提交明确失败

- 在同一次可靠状态转换中保存本地 `FAILURE + refund_pending`，不能先写终态再把退款只留在内存
- 退款 worker 通过唯一计费事件对预扣执行一次全额退款
- 退款失败保留 `refund_pending` 重试，不发生重复退款或漏退款

#### 上游返回 402

- 解释为 Seedance 渠道账户余额不足，而不是本站用户余额不足
- 保存 `FAILURE + refund_pending`，全额幂等退回本站预扣
- 标记渠道资金异常并通知管理员；同一渠道不继续重试提交
- 对客户端固定返回 502 `upstream_balance_insufficient` 渠道错误，不返回本站余额不足的 402

#### 上游提交成功

- 成功任务最终本站费用等于请求提交前冻结的预扣费用
- 完成后不补扣，不读取上游实际费用
- 任务失败、取消或超时则全额退款
- Context IR 与普通视频遵循同一计费状态机，只是成功产物为 `result_text`

#### 上游提交结果不确定

网络在发送请求后中断时，无法确认上游是否创建任务，而且公开文档没有声明幂等键。此时：

- 不自动重试提交，避免生成两个收费任务
- 在本地同时标记 `SUBMIT_UNKNOWN + refund_pending`
- 由退款 worker 向用户幂等退款并记录管理员审计事件
- 将潜在的上游孤儿任务作为运营损失处理
- 如果后续上游确认支持 `Idempotency-Key`，再改为相同幂等键重试

### 9.4 持久化计费状态

为消除“任务已终态但未退款”的窗口，Task 增加独立计费状态：

```text
reserve_pending
reserved
settled
refund_pending
refunded
```

同时保存不可变的价格快照：

- 原始模型名和上游模型名
- 本站 ModelPrice
- 计费乘数
- 分组倍率
- 预扣 quota
- 价格配置版本或哈希
- 报价依据
- 是否发生 quota saturation

增加任务计费事件表或等价的唯一幂等记录，唯一键建议为：

```text
task:{local_task_id}:reserve
task:{local_task_id}:refund
task:{local_task_id}:settle
```

Seedance 任务不能继续依赖仅存在于请求内存中的 `BillingSession.refunded` 标志，也不能让 `BATCH_UPDATE` 延迟写入成为账务真值。预扣、退款和成功确认采用任务专用 durable 模式：

1. 在主库事务中锁定 Task、计费事件以及对应用户额度或订阅记录和 Token 记录。
2. 先创建唯一 `prepared` 事件；若唯一键已存在则读取其阶段，不重复应用资金变化。
3. 直接写主库资金和 Token 额度，禁止该路径进入异步 batch update；同事务更新 Task 的 billing_status、reserved_quota 和事件为 `money_applied`。
4. 事务提交后再刷新或失效 Redis/内存缓存。缓存失败只告警并重建，数据库仍是账务真值。
5. 消费/退款日志若使用独立日志库，标记 `log_pending` 并幂等补写；日志失败不能回滚或重复执行资金变化，补写成功后事件转 `completed`。

订阅预扣沿用现有订阅记录语义，但需要增加可接收事务的 model 方法，使订阅额度变化与事件阶段同库提交。所有行锁统一走 `lockForUpdate(tx)`；SQLite 依靠事务串行化且不生成不支持的 `FOR UPDATE`。

轮询任务不仅扫描执行未终态任务，也扫描 `refund_pending` 等账务未完成任务。资金调整失败时保留待处理状态，由后台重试，不能只写日志后放弃。

完整状态转换如下：

| 场景 | 任务状态 | 计费状态 |
| --- | --- | --- |
| 本地任务已创建、尚未预扣 | `SUBMITTING` | `reserve_pending` |
| 最大费用预扣成功、准备请求上游 | `SUBMITTING` | `reserved` |
| 上游接单 | `QUEUED` 或 `SUBMITTED` | `reserved` |
| 视频或 Context IR 成功 | `SUCCESS` | `settled` |
| 上游拒绝、402、任务失败、取消或超时 | `FAILURE` 或 `SUBMIT_UNKNOWN` | `refund_pending` |
| 全额退款完成 | 保持失败终态 | `refunded` |

任何路径都不允许从 `settled` 回到退款，也不允许在没有唯一幂等事件的情况下直接调整用户额度。

### 9.5 日志与统计

每条提交、成功、退款日志至少包含：

- 本地 task_id
- channel_id
- origin_model 和 upstream_model
- reserved_quota 和 final_quota
- 本站价格和分组倍率快照
- batch_size 等计费乘数
- 退款原因
- 幂等事件 ID
- quota saturation 管理员审计标记

`request_count` 每个任务只增加一次。退款要冲减用户和渠道的净 used_quota，但不重复减少 request_count。

## 10. 任务与响应设计

### 10.1 ID 隔离

- 客户端永远只看到本地 `task_xxx`
- 上游 ID 只保存在 `TaskPrivateData.UpstreamTaskID`
- 日志默认不输出上游 ID；管理员审计可显示脱敏值
- 查询接口禁止使用上游 ID 查找本地任务

### 10.2 状态映射

| Seedance | 本地 TaskStatus | 下游 OpenAI Video |
| --- | --- | --- |
| `queued` | `QUEUED` | `queued` |
| `in_progress` | `IN_PROGRESS` | `in_progress` |
| `completed` | `SUCCESS` | `completed` |
| `failed` | `FAILURE` | `failed` |
| Context IR `SUCCESS` | `SUCCESS` | 旧版查询返回 `SUCCESS + result_text` |
| Context IR `FAILURE` | `FAILURE` | 旧版查询返回 `FAILURE + error` |
| Midjourney `SUBMITTED`/`NOT_START` | `SUBMITTED` | 对应 MJ 查询格式 |
| Midjourney `SUCCESS` | `SUCCESS` | `SUCCESS` |
| Midjourney `FAILURE`/`CANCEL` | `FAILURE` | `FAILURE` |

状态更新必须单调。已经进入 `SUCCESS` 或 `FAILURE` 的任务不能被上游偶发的旧响应改回运行中。

### 10.3 下游查询响应

普通视频响应保持 OpenAI Video 风格：

- `id` 使用本地 ID
- `model` 使用用户请求的原始模型名
- `status` 和 `progress` 来自本地数据库
- 成功时 `metadata.url` 指向本站 `/v1/videos/{id}/content`
- 失败时返回稳定的错误 code 和脱敏 message

Context IR 的 `GET /v1/video/generations/{local_task_id}` 返回本地任务状态；成功时在 `data.result_text` 返回增强后的提示词，不提供视频 URL 或 `/content`。用户查询不触发实时上游请求。

上游原始响应不能原样写入公开 `Task.Data`。写入前必须通过结构化 DTO/JSON 遍历构造最小安全数据，不能用字符串替换；以下规则递归作用于嵌套对象和数组：

- 删除 `id`、`task_id`、`upstream_task_id` 等所有上游任务标识
- 删除 `cost`、`quota`、`thirdPartyConsumeMoney` 和其他上游费用字段
- 删除所有上游签名 URL 的 query 和 fragment
- 只保留业务必需的 `status`、`progress`、脱敏 `error`、`result_text`、`video_urls`、本站代理后的 `last_frame_url`，以及 catalog 声明的本地 `artifact_*` 句柄
- `Task.Data.video_urls` 只能保存本站代理地址，不能保存上游临时直链

下载所需的完整上游签名 URL 只能放在不返回用户的 `TaskPrivateData.ResultURL`。Midjourney 批量结果需要新增私有 `ResultURLs []string`，最后一帧等附属媒体放私有 `ResultAssets`，公开响应再按 index 或 asset 生成本站代理地址。普通日志、用户任务接口和管理员非敏感详情都不能读取或输出这些私有原始 URL。

Midjourney Video 查询保留 `video_url` 和 `video_urls` 结构，但 URL 改为本站代理地址。批量视频可使用：

```text
/v1/videos/{local_task_id}/content?index=0
/v1/videos/{local_task_id}/content?index=1
```

### 10.4 结果过期

首期不做转存，因此结果过期是已接受的边界：

- 保存 `result_expires_at`，能从签名参数解析时使用实际值，否则按 24 小时估算
- 下载时发现上游 403、404 或签名过期，返回 410 Gone
- 任务生成成功后结果过期不自动退款
- 前端任务页显示下载按钮和到期时间，不在日志列表直接暴露签名 URL

## 11. 轮询与错误处理

### 11.1 调度

新增独立的 `seedance_video_poll` 系统任务：

- 默认间隔 5 秒，与上游建议一致
- 使用现有数据库 lease，保证多实例只有一个有效执行者
- 只扫描 Seedance platform，避免改变其他供应商的轮询频率
- 通用异步轮询排除 Seedance platform，防止重复查询
- 同一任务用状态 CAS，保证终态副作用只触发一次

### 11.2 重试分类

现有轮询会在 debug 日志输出完整上游响应。Seedance 路径必须改为只记录 task kind、脱敏状态、进度、HTTP 状态和错误码，禁止记录原始 response body、完整结果 URL、continuation artifact 或费用字段。

| 情况 | 处理 |
| --- | --- |
| 网络超时、连接失败 | 保持原状态，指数退避 |
| 429 | 保持原状态，读取 Retry-After，加入抖动 |
| Seedance 402 | 标记渠道资金异常并告警；任务保持可恢复状态，不伪装成本站用户欠费；持续到任务超时后进入 `refund_pending` |
| 500、502、503、504 | 保持原状态并退避 |
| 401、403 | 标记渠道认证异常并告警，不立即把视频任务判失败 |
| 首次 404 | 考虑上游最终一致性，短暂重试 |
| 连续 404 | 标记轮询异常，达到任务超时后退款 |
| 合法 `failed` | 终态失败并进入退款 |
| 普通视频 `completed` 但 URL 为空 | 暂不成功，短暂重试；超过阈值后失败退款并告警 |
| Context IR `SUCCESS` 但 result_text 为空 | 暂不成功，短暂重试；超过阈值后失败退款并告警 |
| 未知状态 | 不覆盖原状态，记录脱敏响应并告警 |

### 11.3 超时

- 继续使用 `TASK_TIMEOUT_MINUTES`，默认 1440 分钟
- `UPDATE_TASK` 必须开启；启动时若关闭而存在 Seedance 渠道，记录明确告警
- 超时任务进入 `FAILURE + refund_pending`，由幂等退款 worker 完成退款
- 渠道被禁用或暂时读取失败时不能直接终态失败而跳过退款

## 12. 素材上传安全设计

- 在读取 multipart 前设置 50 MB 硬上限，并为 multipart 开销保留小幅余量
- 同时检查扩展名、声明 MIME 和文件魔数
- 文件名只用于 Content-Disposition，不用于本地路径
- 只允许文档声明的图片、音频和视频格式
- 以流式方式转发，不落盘、不长期缓存
- 首要限流键是 `channel_id + 上游 Key 指纹`，所有用户和本站 Token 共享上游规定的 10 次/分钟、200 次/天额度；绝不能只按本站用户或 Token 限流
- 限流计数必须跨实例原子共享，并保守采用滚动 60 秒/24 小时窗口，避免固定窗口边界突发；Redis 可用时用单个 Lua 操作维护时间序列，无 Redis 时使用数据库 guard 行、请求时间记录和 `lockForUpdate(tx)`，不能退化为每实例内存计数
- 只保存不可逆 Key 指纹，不把原始上游 Key 写入限流键、数据库或日志
- 可以再叠加更低的本站用户级分钟/日限额，防止单个用户耗尽共享额度，但它不能替代上游 Key 聚合限流
- 上游 429 原样转换为稳定的本站错误
- 上传日志只记录用户、大小、类型、耗时和状态，不记录完整签名 URL
- 上传响应 URL 仅返回给发起用户，不写入普通消费日志

## 13. 下载代理改造

`controller/video_proxy.go` 需要完成以下改造：

- Seedance 直接使用任务保存的结果 URL，不拼接上游 `/content`
- 支持 `Range`、`If-Range` 和 206 响应
- 只转发安全响应头，不复制 Cookie、认证或上游内部头
- 对批量视频校验 `index` 边界
- 流式传输，不缓冲完整视频
- 区分预览和下载；下载时设置安全的 `Content-Disposition`
- 将总请求超时改成适合大文件流式传输的策略
- URL 日志统一移除 query 和 fragment，防止签名泄露
- 使用 `Cache-Control: private`，不把用户视频标记为公共缓存
- 保留现有用户归属校验和 SSRF 防护

## 14. 模型发现与渠道管理

### 14.1 渠道注册

需要修改：

- `constant/channel.go`：渠道类型、名称、默认 Base URL、Dummy 边界
- `relay/relay_adaptor.go`：注册 Seedance TaskAdaptor
- `common/endpoint_type.go`：Seedance 只声明 OpenAI Video endpoint
- `common/endpoint_defaults.go`：补充 `EndpointTypeOpenAIVideo -> POST /v1/videos`
- `controller/model.go`：显式从 Seedance catalog 注册默认模型和 channel type 对应模型，不能依赖同步 Adaptor 初始化

不要给 Seedance 注册普通 Chat `APIType`，避免系统把该渠道当成聊天渠道。旧版 Context IR 和 Midjourney Video 是该渠道的专用任务协议接线，不代表它支持普通聊天端点。

渠道更新或删除前检查未终态 Seedance 任务：存在任务时默认阻止更换 Key、Base URL 或删除渠道，允许仅修改名称、标签等无关字段。为处理 Key 泄露等紧急情况，可提供管理员显式强制操作；强制前把受影响任务列入审计并转 `refund_pending`，提示可能产生上游孤儿任务。任务保存提交时 Key 指纹用于轮询前一致性检查，但不保存原始 Key。

### 14.2 模型拉取

复用现有 `fetchChannelUpstreamModelIDs` 的 Bearer `/v1/models` 请求。该接口无 Key 返回 401；当前公开资料未承诺响应会携带可靠的 modality 或 endpoint 元数据，因此只把它当“模型 ID 候选发现”，不能靠上游响应字段自动判断视频能力。

候选 ID 必须与本地 catalog 分类：

- `video_output`：105 个主视频输出/超分模型，可选择加入渠道
- `video_prompt_enhancer`：3 个 Context IR 模型，可选择加入渠道，但不计入 106 个视频输出数
- `non_video`：不显示在 Seedance 视频模型选择列表
- `unknown`：显示为待适配，不允许直接启用或自动同步
- `midjourney-video` 作为本站明确的视频 SKU 单独加入

模型拉取只发现候选，不自动赋予能力或配置价格。运行时以本地 catalog 为安全真值；未知新模型必须先补端点分类、参数边界、结果解析和本站价格才能启用。未定价模型即使在渠道中启用，也不能对用户调用上游。

### 14.3 渠道测试

自动渠道测试必须调用只读的 `/v1/models`，不得通过生成视频测试连通性，因为生成测试会产生真实上游费用。测试只验证：

- DNS、TLS 和代理
- Bearer Key 有效性
- `/v1/models` 返回可解析结构
- 至少存在一个本地已识别的视频模型

本次不调用钱包接口。

## 15. 前端与 i18n

需要补充：

- `web/src/features/channels/constants.ts`：渠道类型和排序
- `web/src/features/channels/lib/channel-type-config.ts`：默认 Base URL、Key 提示和模型提示
- `web/src/features/channels/lib/channel-utils.ts`：使用现有图标系统或 Lucide 视频图标
- `MODEL_FETCHABLE_TYPES`：加入 Seedance
- Seedance 渠道表单隐藏多 Key 模式并解释单 Key 限制；后端仍做强制校验
- 任务日志 platform 显示 Seedance，不显示数字 61
- 模型详情页按 catalog 分类展示示例：主视频用 `/v1/videos`，Context IR 用旧提交/查询和 `result_text`，Midjourney Video 用专用路径；不要误展示 Chat API
- 任务详情显示状态、进度、下载按钮和预计过期时间

所有新增用户可见文案必须使用 `useTranslation()`，并同步以下 locale：

```text
en, zh, zh-TW, fr, ja, ru, vi
```

实施前必须读取并遵循 `web/AGENTS.md` 和项目的 i18n skill。

## 16. 文件级实施清单

### 16.1 新增文件

```text
relay/channel/task/seedance/adaptor.go
relay/channel/task/seedance/dto.go
relay/channel/task/seedance/catalog.go
relay/channel/task/seedance/validate.go
relay/channel/task/seedance/billing.go
relay/channel/task/seedance/response.go
relay/channel/task/seedance/context_ir.go
relay/channel/task/seedance/midjourney_video.go
relay/channel/task/seedance/upload.go
relay/channel/task/seedance/adaptor_test.go
relay/channel/task/seedance/validate_test.go
relay/channel/task/seedance/billing_test.go
relay/channel/task/seedance/response_test.go
controller/seedance_upload.go
```

可靠计费事件和无 Redis 时的上传聚合限流需要持久化记录，再新增：

```text
model/task_billing_event.go
model/task_artifact.go
model/provider_rate_limit.go
service/task_billing_worker.go
```

### 16.2 修改文件

```text
constant/channel.go
common/endpoint_type.go
common/endpoint_defaults.go
router/video-router.go
middleware/distributor.go
relay/constant/relay_mode.go
relay/relay_adaptor.go
relay/relay_task.go
relay/channel/adapter.go
controller/relay.go
controller/model.go
controller/channel.go
controller/channel_upstream_update.go
controller/channel-test.go
controller/system_task_handlers.go
controller/video_proxy.go
model/task.go
model/channel.go
model/main.go
model/system_task.go
model/user.go
model/token.go
model/subscription.go
service/task_polling.go
service/task_billing.go
web/src/features/channels/constants.ts
web/src/features/channels/lib/channel-type-config.ts
web/src/features/channels/lib/channel-utils.ts
web/src/features/usage-logs/components/columns/task-logs-columns.tsx
web/src/features/pricing/components/model-details-api.tsx
web/src/i18n/locales/*.json
```

实际实施时只修改确有需要的文件，不为匹配本清单进行无意义重构。

## 17. 数据库兼容

所有 Task 新字段和计费事件表必须通过 GORM 实现，同时支持 SQLite、MySQL 5.7.8+ 和 PostgreSQL 9.6+。

建议新增字段：

- `billing_status`
- `reserved_quota`
- `next_poll_at`
- `poll_failures`
- `result_expires_at`

`TaskPrivateData` 增加批量视频所需的私有 `ResultURLs []string` 和附属媒体 `ResultAssets`。不可变价格快照可以继续放在 `TaskPrivateData.BillingContext`，但需要扩展为视频报价快照。需要参与查询、CAS 或重试的状态必须放普通列，不能依赖跨数据库 JSON 查询。

`TaskArtifact` 持久化随机唯一的公开 `artifact_id`、只供服务端读取的 `upstream_value`、kind、user_id、task_id、channel_id 和过期时间，并对 `artifact_id` 建唯一索引。客户端只接收和回传 `artifact_id`；服务端按句柄查找后验证所有权、来源任务终态、渠道、kind 和有效期，再把 `upstream_value` 写入上游请求。该值不属于 API Key，但仍按私有任务数据管理：模型序列化默认隐藏，任何用户/管理员通用列表接口都不返回，普通日志和 `Task.Data` 都不得记录。项目目前没有可逆加密设施，本阶段不虚设“加密存储”前提；部署侧依赖数据库静态加密和访问控制，若以后引入统一字段加密，再单独迁移该私有列。

上传聚合限流的数据库回退包含每个 `(provider, channel_id, key_fingerprint)` 的唯一 guard 行和最多保留 24 小时的请求时间记录。事务锁定 guard、清理过期记录、同时检查 60 秒与 24 小时计数并插入本次记录。它只在 Redis 不可用时承担跨实例原子计数，不保存原始 Key。

要求：

- 不使用数据库专属 JSON 操作符
- 不使用数据库专属自增语法
- 唯一幂等键使用 GORM 唯一索引
- 行锁使用项目 `lockForUpdate(tx)`
- SQLite 不使用不支持的 `ALTER COLUMN`
- 不增加会导致 GORM 重复迁移的布尔默认标签

## 18. 测试计划

### 18.1 请求验证

- 每个模型家族至少一组成功请求和全部关键边界失败请求
- t2v、i2v、multi、r2v、v2v、edit、motion、lip-sync、upscaler
- catalog 逐项断言覆盖 105 个主视频输出、3 个 Context IR 和 1 个 Midjourney Video，且每项都有唯一协议分类
- 3 个 Context IR 分别覆盖 prompt、seconds、ratio、图片和多模态素材边界
- Zhenzhen 本地 extend task ID 转换、FLUX draft_cache 归属、Kling session/face artifact 归属和过期边界
- prompt 必填和可选场景
- `seconds` 字符串、`-1`、固定枚举和上下界
- Seedance 2.0 与 2.5 不同的素材上限
- metadata 与顶层重复字段的归一化
- 显式 `false`、`0` 不被 `omitempty` 丢失
- catalog 允许的 opaque `json.RawMessage` 嵌套值无损往返，白名单外字段被拒绝
- 未知模型、未知计费乘数和不支持的 metadata 字段
- batch_size 只接受 1、2、4，任意无符号数量字段的巨大整数都在访问上游前返回 400
- 现有 `/v1/videos/{id}/remix` 选中 Seedance 时在本地拒绝且上游请求数为 0
- Seedance 渠道创建和更新均拒绝多 Key 配置
- 存在未完成 Seedance 任务时默认拒绝修改 Key/Base URL 或删除渠道；显式强制路径会审计并把受影响任务转为全额退款

### 18.2 上游协议契约

使用 `httptest.Server` 模拟：

- `/v1/videos` 的 200/201/202 成功、4xx、5xx、错误 JSON、缺少 task ID
- `/v1/videos/{id}` 的四种状态和未知状态
- Context IR 旧提交/查询端点、`SUCCESS + result_text`、`FAILURE` 和空结果
- `/v1/files/upload` 的 multipart 转发和 50 MB 限制
- 多个本站用户和多实例共享同一上游 Key 时，合计第 11 次/分钟及第 201 次/天在访问上游前被拒绝
- Midjourney Video 的提交、批量结果和失败结果
- 上游提交 `402 payment_required` 被映射为 502 `upstream_balance_insufficient` 并进入全额退款，绝不返回本站用户余额不足 402
- Authorization 和 Content-Type 正确
- 模型映射后发送的是 upstream model
- 客户端响应绝不包含 upstream task ID
- return_last_frame 被改写为本站 asset 代理，continuation artifact 只返回 catalog 允许字段

### 18.3 计费

- 未配置 ModelPrice 时不访问上游
- 最大预扣不足时不访问上游
- 本站普通用户额度、订阅和 Token 预扣一致
- group ratio 在提交时冻结
- batch 1、2、4 的准确预扣
- Context IR 每个 SKU 使用本站固定最大价，成功不按文本长度补扣
- `seconds=-1` 和 upscaler 使用配置上限
- 上游提交失败只退款一次
- 任务失败和超时只退款一次
- 成功任务不发生完成后补扣
- 退款临时失败可重试
- 开启 `BATCH_UPDATE` 时 Seedance durable 计费仍直接落主库；进程重启和缓存刷新失败均不重复扣退
- 本站普通用户额度与订阅两种资金来源的 reserve/refund 事件在 SQLite、MySQL、PostgreSQL 上保持唯一且可恢复
- quota 转换溢出、NaN、Inf 不产生负费用并写入管理员审计
- request_count 只增加一次，used_quota 与最终净费用一致

### 18.4 轮询并发与恢复

- 两个节点同时轮询，只有一个节点获得终态 CAS
- 终态落库后进程退出，退款 worker 能恢复
- 资金调整成功但事件回写失败，重试不重复退款
- 429、5xx 和网络错误不误判任务失败
- 轮询收到上游 402 时标记渠道异常，任务可恢复并在最终超时时全额退款
- 渠道暂时不可读不会吞掉退款
- 状态倒退不会覆盖新状态
- completed 缺 URL 不会误标成功

### 18.5 下载和安全

- 只有任务所有者或合法管理员可以下载
- Range 请求返回 206
- index 越界返回 400
- SSRF 私网地址被拦截
- 日志不含签名 query
- `Task.Data` 不含上游 task ID、`cost`、`quota`、`thirdPartyConsumeMoney` 或上游签名 URL，批量结果只暴露本站代理 URL
- 伪造、跨用户、跨渠道、类型不匹配或过期的 workflow artifact 在访问上游前返回 400
- 临时 URL 过期返回 410
- 大文件流式传输不产生整文件内存占用

### 18.6 前端

- Seedance 渠道可创建和编辑
- 可拉取并按 `video_output`、`video_prompt_enhancer`、`non_video`、`unknown` 分类；未知模型不能自动启用
- 默认模型清单完整包含 106 个视频输出和 3 个 Context IR，不依赖同步 Chat Adaptor
- 任务日志显示正确渠道名
- 视频示例不是 Chat API 示例
- 下载按钮、加载、失败和过期状态完整
- 七个 locale 无缺失 key

### 18.7 验证命令

```bash
go test ./relay/channel/task/seedance/... ./service/... ./controller/... ./model/...
go test ./...

cd web
bun run i18n:sync
bun run lint
bun run build
```

本方案不计划修改 `relaykit/`。如果实施时触及 `relaykit/` 或其公共接口，必须额外执行：

```bash
cd relaykit
GOWORK=off go build ./...
```

SQLite 在本地测试；MySQL 和 PostgreSQL 的迁移、唯一幂等键和并发 CAS 在 CI 集成测试中验证。

## 19. 实施阶段

### 阶段 1：渠道和协议骨架

- 新增 ChannelTypeSeedance 和默认 Base URL
- 注册 TaskAdaptor、OpenAI Video endpoint、Context IR 旧协议和 Midjourney Video 专用 relay mode
- 在 `controller/model.go` 显式接入 106 个视频输出与 3 个 Context IR 的 catalog
- 增加前端渠道类型
- 接通只读模型拉取和只读渠道测试

交付标准：管理员可以创建 Seedance 渠道并拉取视频模型，但未配置价格的模型不能调用。

### 阶段 2：主视频提交

- 完成专属 DTO、能力注册表和模型级校验
- 完成 `/v1/videos` 上游提交
- 完成 3 个 Context IR 的旧协议提交、查询转换和 `result_text` 持久化
- 完成视频内部多步依赖的本地任务 ID 转换与 continuation artifact 归属校验
- 完成模型映射后校验
- 完成固定本站价格和最大费用预扣
- 延迟客户端成功响应直到本地 Task 持久化

交付标准：测试上游可提交任务，本地和上游 ID 严格隔离，余额不足不会产生上游任务。

### 阶段 3：可靠轮询和退款

- 新增 Seedance 5 秒轮询任务，并按主视频、Context IR、Midjourney Video 三种协议查询
- 完成状态映射、退避和单调更新
- 增加 billing_status 和幂等计费事件
- 修复终态后退款不可恢复的问题

交付标准：成功任务完成、失败任务全额退款；并发轮询和进程中断不产生重复退款或漏退款。

### 阶段 4：素材上传和下载

- 增加 `/v1/files/upload`
- 完成流式上传、大小和 MIME 校验，以及上游 Key 聚合的 10/分钟、200/天跨实例限流
- 完善 Range 下载、批量视频 index 和 URL 脱敏
- 支持最后一帧等 catalog 声明附属媒体的私有存储与 asset 代理

交付标准：用户可上传本地素材、生成视频并通过本站代理下载，不暴露上游 Key 或签名日志；多个用户不能合计突破单上游 Key 的上传额度。

### 阶段 5：Midjourney Video

- 增加 Video 专用提交和查询路由
- 复用 Seedance channel、Task 表、预扣和轮询
- 完成 batch_size 计费和多结果下载

交付标准：直接图片 URL 的 Midjourney i2v 可以提交、轮询和下载 1、2、4 个结果；不开放非视频动作。

### 阶段 6：前端、全量测试和灰度

- 完成渠道 UI、任务日志、模型示例和 i18n
- 完成三数据库测试
- 使用一个显式授权的低价视频任务进行人工端到端验收
- 先只开放少量管理员测试 Token，再按模型逐步开放

自动化测试和自动渠道检测不得生成付费视频。真实付费 smoke test 必须由管理员明确触发。

## 20. 上线验收标准

- catalog 中 105 个主视频输出、1 个 Midjourney Video 和 3 个 Context IR 均有明确协议、校验和本站价格；所有已启用且已定价能力可提交
- 非视频模型无法通过 Seedance 视频渠道路由
- 本地素材可以上传并用于视频请求
- Zhenzhen extend、FLUX draft-enhance、Kling lip-sync 等视频内多步链路不暴露或接受未归属的上游标识
- 所有用户输入数量和时长在计费前有明确上限
- 余额不足时上游请求数为 0
- Seedance 上游 402 被识别为渠道错误并全额退款，不会冒充本站用户余额不足
- 客户端只看到本地 task ID
- 用户查询只读本地状态，后台持续轮询上游
- Context IR 成功查询返回 `result_text`，不错误生成下载地址
- 成功任务保留本站预扣费用，失败和超时任务全额且仅退款一次
- 任意进程中断后，待退款账务可以恢复
- 视频可流式预览、下载和断点续传
- 普通日志和 `Task.Data` 不包含 API Key、上游 task ID、上游费用字段或签名 URL query
- 渠道测试不产生上游费用
- SQLite、MySQL、PostgreSQL 均可迁移和运行
- 前端七种语言无新增缺失文案

## 21. 已知边界与后续项

以下内容不阻塞首期接入，但需要在产品说明中明确：

1. 上游没有公开稳定的任务级实际费用字段，因此本站按独立售价结算，运营方自行承担上游成本波动。
2. 上游没有声明提交幂等键，网络结果不确定时无法做到上游任务绝对零孤儿，只能不重试并退款用户。
3. 视频结果使用临时签名 URL，首期不保证长期可下载。
4. Midjourney 的 `task_id + index` 输入依赖非视频图片任务，本次只支持直接图片 URL 的视频路径。
5. 单 Key 模式下任务轮询使用渠道当前 Key；存在未完成任务时默认阻止修改 Key/Base URL 或删除渠道，紧急强制操作会退款并承担潜在上游孤儿成本。
6. 上游新增模型只有在本地能力规则和本站价格都配置后才能开放。

后续对象存储接入时，建议在现有下载代理后增加 `ResultStorage` 接口，由轮询完成事件异步转存，不修改 Seedance 适配器的请求和状态逻辑。
