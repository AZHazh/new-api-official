/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
export const TIANDOU_DOC_TITLE = '甜豆 AI 使用教程'

export const TIANDOU_DOC_CONTENT = `
# 甜豆 AI 使用教程

这是一份面向新用户的快速上手文档，覆盖注册、充值、创建 API 令牌、环境检查与 Codex 配置。
> 建议先完整阅读一遍，再开始实际配置。尤其是令牌分组、Node.js 环境和 \`config.toml\` 配置，这三处最容易影响后续使用。

## 1. 注册账号

注册链接：
[https://www.tiandouai.com/register](https://www.tiandouai.com/register)

![注册页面](/doc-assets/tiandou/image1.png)

1. 填写用户名、密码、邮箱。
2. 点击“获取验证码”后，到填写的邮箱里查看验证码并填回注册页。
3. 确认填写无误后，点击“注册”即可。
4. 注册成功后会自动跳转到登录页面。

## 2. 登录账号

登录入口：
[https://www.tiandouai.com/login](https://www.tiandouai.com/login)

![登录页面](/doc-assets/tiandou/image2.png)

1. 输入邮箱地址或用户名。
2. 输入账号密码。
3. 点击“继续”完成登录。

## 3. 购买额度

登录控制台后，进入左侧“钱包管理”页面购买额度。

![钱包管理](/doc-assets/tiandou/image3.png)

1. 在“选择充值额度”中选择固定金额，或在“自定义额度”中输入要充值的金额。
2. 选择支付方式，点击“支付宝”后按页面提示完成支付。

> 支付说明
>
> 当前充值比例为 1:1，即 1 元人民币约等同于 1 美元额度。选择优惠档位时，实际支付会更少。如果没有弹出支付页面，请先关闭代理后重试。

## 4. 创建 API 令牌

登录后进入控制台面板，左侧选择“令牌管理”。

![令牌管理入口](/doc-assets/tiandou/image4.png)

### 4.1 进入令牌管理

1. 在左侧菜单点击“令牌管理”。
2. 点击页面上方的“添加令牌”。

### 4.2 创建新令牌

在弹窗中填写令牌信息：

![创建新令牌](/doc-assets/tiandou/image5.png)

- 令牌名称：可自定义，用于区分不同用途，例如 Claude Code、Codex、Gemini。
- 令牌分组：必须选择，分组决定这个令牌可以使用哪些模型。
- 过期时间：默认“永不过期”，也可以按需要设置有效期。
- 新建数量：通常保持 1 即可。
- 额度设置：即便开启“无限额度”，令牌实际可用额度仍受账户余额限制。
- 访问限制：如果暂时不熟悉，建议先保持默认，不要额外开启模型限制或 IP 白名单。

> 令牌分组很重要
>
> 令牌分组会直接影响可调用模型。比如 Claude Code、Codex、Gemini CLI 都需要匹配对应分组；如果分组选错，后续 CLI 配置时很容易出现“模型不存在”或无法调用的问题。

填写完成后，点击右下角“提交”完成创建。

### 4.3 查看分组可用模型

你可以在“模型广场”里查看每个令牌分组下支持哪些模型。

![模型广场](/doc-assets/tiandou/image6.png)

1. 点击页面右上角“模型广场”。
2. 在左侧“可用令牌分组”中选择分组，即可看到对应分组支持的模型列表。

## 5. 环境检查

在配置 Claude Code、Codex 或 Gemini CLI 之前，请先确认本机已经正确安装 Node.js。

在 Windows、macOS 或 Linux 终端执行：

\`\`\`bash
npm list -g --depth-0
\`\`\`

如果命令可以正常执行，说明 Node.js 和 npm 已经可用。即使输出中没有安装任何全局包，也不影响后续配置。

如果提示“命令未找到”或类似错误，说明当前环境还没有安装 Node.js，或安装后没有正确加入系统环境变量。请先完成 Node.js 安装，再重新执行上面的命令确认。

> 环境检查很重要
>
> CLI 工具依赖 Node.js 和 npm。环境没有准备好时，后续安装 Claude Code、Codex、Gemini CLI 都可能失败。

## 6. Codex 配置教程

Codex 官网地址：[点击访问 Codex 官网](https://openai.com/zh-Hans-CN/codex/)

### 6.1 安装 Codex

#### 安装桌面版 Codex

![Codex 官网](/doc-assets/tiandou/image7.png)

打开 Codex 官网，按系统提示下载安装即可。

#### 在终端安装

步骤如下：

在 Windows 或 macOS 终端输入以下命令，等待安装完成：

\`\`\`bash
npm i -g @openai/codex
\`\`\`

检查是否安装成功，终端中继续输入：

\`\`\`bash
codex
\`\`\`

![Codex 命令行启动](/doc-assets/tiandou/image8.png)

如果出现对应启动选项，说明安装成功。按提示继续，即可进入 Codex。

### 6.2 Codex 配置

#### Windows 环境

按下 \`Win + R\`，输入以下内容后回车，打开 Codex 配置目录：

\`\`\`powershell
%userprofile%\\.codex
\`\`\`

![Windows 打开 Codex 目录](/doc-assets/tiandou/image9.png)

在 \`.codex\` 目录下，需要准备两个文件：\`config.toml\` 和 \`auth.json\`。如果没有，就新建。

- \`config.toml\`：Codex 的核心配置文件，中转服务和 MCP 等都在这里配置。
- \`auth.json\`：用于保存你在中转站获取的 API Key。

配置 \`config.toml\`

将以下内容写入 \`config.toml\`：

\`\`\`toml
disable_response_storage = true
model = "gpt-5.5"
model_provider = "tiandouAI"
model_reasoning_effort = "xhigh"
preferred_auth_method = "apikey"

[model_providers.tiandouAI]
base_url = "https://www.tiandouai.com/v1"
name = "tiandouAI"
wire_api = "responses"
\`\`\`

配置 \`auth.json\`

登录控制台，在“令牌管理”里复制密钥。

![复制 API Key](/doc-assets/tiandou/image10.png)

将以下内容写入 \`auth.json\`：

\`\`\`json
{
  "OPENAI_API_KEY": "你的 API Key"
}
\`\`\`

测试对话：

桌面版 Codex：
打开桌面版 Codex，在聊天框输入一段测试消息。如果可以正常回复，说明配置成功。

![桌面版 Codex 测试](/doc-assets/tiandou/image11.png)

终端版 Codex：

\`\`\`bash
codex
\`\`\`

启动后输入测试对话；如果能正常返回内容，说明配置完成。

![终端版 Codex 测试](/doc-assets/tiandou/image12.png)

#### macOS 环境

在访达界面按下 \`Command + Shift + G\`，输入以下路径后回车，打开 Codex 配置目录：

\`\`\`bash
~/.codex
\`\`\`

![macOS 打开 Codex 目录](/doc-assets/tiandou/image13.png)

在 \`.codex\` 目录下，同样需要准备 \`config.toml\` 和 \`auth.json\` 两个文件。

![macOS 配置目录](/doc-assets/tiandou/image14.jpeg)

- \`config.toml\`：Codex 的核心配置文件。
- \`auth.json\`：用于保存你在中转站复制的 API Key。

配置 \`config.toml\`

将以下内容写入 \`config.toml\`：

\`\`\`toml
disable_response_storage = true
model = "gpt-5.5"
model_provider = "tiandouAI"
model_reasoning_effort = "xhigh"
preferred_auth_method = "apikey"

[model_providers.tiandouAI]
base_url = "https://www.tiandouai.com/v1"
name = "tiandouAI"
wire_api = "responses"
\`\`\`

配置 \`auth.json\`

登录控制台，在“令牌管理”中复制密钥。

![复制 API Key](/doc-assets/tiandou/image10.png)

将以下内容写入 \`auth.json\`：

\`\`\`json
{
  "OPENAI_API_KEY": "你的 API Key"
}
\`\`\`

测试对话：

桌面版 Codex：
打开桌面版 Codex，输入一段测试内容，能收到回复就说明配置成功。

![macOS 桌面版 Codex 测试](/doc-assets/tiandou/image15.png)

终端版 Codex：

\`\`\`bash
codex
\`\`\`

启动后输入测试消息。如果能正常工作，说明已经完成接入。

![macOS 终端版 Codex 测试](/doc-assets/tiandou/image16.png)
`
