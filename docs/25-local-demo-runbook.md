# 本地演示与测试数据手册

本文档只适用于本地 MVP 和受控演示环境。账号、密码、数据库密码和对象存储配置均不得复用于任何共享环境或生产环境。

## 1. 默认测试身份

| 入口 | 身份 | 默认值 | 用途 |
| --- | --- | --- | --- |
| Go 主链路（`/platform/v1`） | 本地 owner 身份 | `org_local` / `user_local` / `project_local` | 默认前端工作台、项目、素材库和演示数据 |
| TypeScript 兼容服务（`npm run server`） | 邮箱 | `demo@cookies.local` | 仅兼容/demo-only 的会话登录 |
| TypeScript 兼容服务（`npm run server`） | 密码 | 本机 `COOKIES_DEMO_PASSWORD` | 仅兼容/demo-only 的会话登录 |

Go 主链路当前没有邮箱/密码登录页。它只在 `COOKIES_ENV=local` 时使用 `.env` 中的 `COOKIES_LOCAL_*` 变量创建受限本地身份，因此启动 Go API 后可直接打开前端。不要试图将上述兼容服务密码作为 Go API 或生产系统密码。

如确有本地演示需要，可在未提交的 `.env` 中改写 `COOKIES_DEMO_EMAIL` 和 `COOKIES_DEMO_PASSWORD`；两者只被 TypeScript 兼容服务读取。Go 主链路仍由 `COOKIES_LOCAL_*` 控制。

## 2. 演示数据集

设置 `COOKIES_DEMO_DATA_DIR` 后执行 `npm run go:seed`，种子程序会：

1. 将目录内所有 `.pdf` 和 `.mp4` 文件写入当前配置的对象存储；
2. 用内容摘要和源文件摘要生成稳定的对象键与资产 ID；即使两份文件字节相同，也会保留两条可展示资产；
3. 向本地 MySQL 注入项目素材、视频时长/尺寸/编码元数据；
4. 创建「Guerlain KOL Brief 解析与视频生成演示」任务，并把 PDF brief 与真实视频资产关联；
5. 写入「导入素材洞察基线」，汇总视频数量、总时长和数据集大小。

当前受控数据集位于 `/Users/bytedance/data`，包含 1 个 28 页 PDF brief 和 30 个 MP4 视频。已验证导入结果为 31 个对象、约 602 MB、约 3,704 秒视频。

```bash
cp .env.example .env
docker compose up -d --wait mysql
COOKIES_DEMO_DATA_DIR=/Users/bytedance/data npm run go:seed
```

本地文件系统对象存储会将对象放在：

```text
COOKIES_FILESYSTEM_BLOB_ROOT/<COOKIES_TOS_ASSETS_BUCKET>/demo/investor/local-data/
```

部署环境应改用已配置的 TOS；不要把 `/Users/bytedance/data`、`.data/` 或真实对象存储凭据提交到仓库。

## 3. 启动与验证

Go 主链路与兼容登录服务需要同时启动：

```bash
go run ./cmd/cookies-api
npm run server
npm run dev
```

前端登录页是 `/login`。它会向兼容服务发出 `POST /api/session`，请求体为：

```json
{"email":"demo@cookies.local","password":"<本机 COOKIES_DEMO_PASSWORD>"}
```

成功后服务端下发仅本地使用的 HttpOnly 会话 Cookie。端口并非默认值时，启动 Vite 时分别设置两条代理：

```bash
VITE_COMPAT_API_PROXY_TARGET=http://127.0.0.1:8787 \
VITE_PLATFORM_PROXY_TARGET=http://127.0.0.1:8080 npm run dev
```

登录入口只验证兼容服务会话；项目工作台请求始终由 Go API 提供。不要将两者指向同一个端口，也不要在此模式下设置 `VITE_API_BASE_URL`。

最低验证项：

```bash
go test ./...
npm run test:server
npm run build
git diff --check
```

在 Go API 已启动时，进入项目 `project_investor_precision_evidence` 的素材库或混剪页，应能看到导入视频；视频预览走受保护的项目资产内容接口，不暴露存储桶或对象键。
## 4. Project 行业与演示链接

演示项目的行业由数据库 `projects.industry` 定义，而不是由前端路由或品牌定义。可用值固定为：

- `short_drama`（短剧）
- `game`（游戏）
- `ecommerce`（电商）
- `automotive_brand`（品牌（汽车））

执行 `npm run go:seed`（或本地 API 启动时的演示 seed）只会向 `project_investor_precision_evidence` 写入演示数据；该 Project 使用 `automotive_brand`。短剧、游戏、电商和品牌（汽车）是新建 Project 可选择的行业，而不是额外的演示数据集。四类 Project 共享二级导航，但需求与策略、创意创作、素材洞察、智能投放分别由行业 schema 决定字段和展示格式。

## 5. 模型 Provider 本地配置（新克隆必读）

模型路由和服务凭据加密保存在**本地 MySQL** 的 `provider_model_routes`、`provider_credentials`、`provider_connections` 三张表里。它们既不随仓库分发，也不由 `npm run go:seed` 写入，因此**任何新克隆的机器上这三张表都是 0 行**。

这不会导致启动失败，但会让所有依赖模型的功能静默退化：`.env.example` 中全部 `COOKIES_PROVIDER_*_ADAPTER` 默认是 `fake`，API 启动日志会打印 `mode=deterministic`，而 `GET /platform/v1/provider/capabilities` 返回：

```json
{"status":"not_configured","capabilities":[]}
```

如果已经配置好的同事能用、你拉下来不能用，先查这里，而不是查代码差异。

### 5.1 生成 Provider 主密钥

主密钥必须是 base64 编码的 32 字节，且只存在于本机未提交的 `.env`，不写入数据库。任何 `scripts/configure-*.ps1` 在它缺失时都会直接抛错。

```bash
openssl rand -base64 32
```

PowerShell 下：

```powershell
$b = New-Object byte[] 32; [System.Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($b); [Convert]::ToBase64String($b)
```

把结果写入 `.env` 的 `COOKIES_PROVIDER_MASTER_KEY`，保持 `COOKIES_PROVIDER_MASTER_KEY_VERSION=v1`。本地 MySQL 数据存在期间不要更换这个值，否则已加密的凭据将无法解密。

### 5.2 写入凭据与模型路由

按团队实际使用的 Provider 选择对应脚本运行一次。脚本会交互式询问 token，只把加密后的凭据材料写进本地 MySQL，不会写入 `.env`：

| 能力 | 脚本 |
| --- | --- |
| 图片（共享 Adapter） | `scripts/configure-adapter-image.ps1` |
| 文本（GPT-5.5 路由） | `scripts/configure-gpt55-text.ps1` |
| 文本（MiniMax） | `scripts/configure-minimax-text.ps1` |
| 视频（Ark Seedance） | `scripts/configure-ark-video.ps1` |
| 语音合成（MiniMax） | `scripts/configure-minimax-speech.ps1` |
| 联网研究（Seed 2.1 Pro） | `scripts/configure-seed-web-research.ps1` |

随后把 `.env` 中对应的 `COOKIES_PROVIDER_*_ADAPTER` 从 `fake` 改成脚本文档标注的值，并重启 `cookies-api`。注意：只要有任一 adapter 不是 `fake`，缺少合法主密钥会让 API **拒绝启动**并提示 `COOKIES_PROVIDER_MASTER_KEY must be base64-encoded 32 bytes`。

### 5.3 验证配置生效

```bash
curl -s -b <会话cookie> http://127.0.0.1:5173/platform/v1/provider/capabilities
```

`status` 应从 `not_configured` 变为已配置，`capabilities` 非空。也可直接查库确认：

```bash
docker exec cookies-mysql-1 mysql -ucookies -p<本机密码> cookies \
  -e "select count(*) from provider_model_routes; select count(*) from provider_credentials;"
```

只做已 seed 的演示走查（浏览项目、预检、审批、审计）时不需要完成本节；确定性假实现足够。

演示工作台的数据由脚本写入正式项目读模型：`platform_project_runtimes` 保存品牌、产品、目标、预算和阶段；`platform_project_workbenches` 及其账户、质检、人工确认、素材版本指针表保存工作台数据。前端通过 `GET /platform/v1/projects/{project_id}/workbench` 读取该 DTO，不再从 `platform_project_operations.fields` 解析 `DEMO-LINK-*` 魔法 JSON。对象存储中的 PDF/视频仍以 Project Asset 版本记录保存。

受控投放 ChangeSet 的唯一数据库表名为 `platform_change_sets`（关联事件表为 `platform_change_set_events`）；文档、测试和 SQL 均使用这一命名。
