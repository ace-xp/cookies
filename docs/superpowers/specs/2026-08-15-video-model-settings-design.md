# 在系统设置页配置视频生成模型（写入数据库）

日期：2026-08-15
状态：设计已确认，待实现

## 一、问题

平台要生成视频，就得有一套「服务地址 + 密钥 + 模型名」。今天配置它只有两条路，都不好走：

1. **PowerShell 脚本** `scripts/configure-ark-video.ps1`。它直接拼 SQL 塞进本地 MySQL 容器，脚本里写死的容器名 `cookies-mysql-1` 和项目实际的 `deployments-mysql-1` 对不上，且要求先手工生成主密钥。运维能用，产品负责人用不了。
2. **`.env` 直连**（2026-08-15 本次会话前半程新增）。改三行环境变量即可，但要能碰到服务器文件、且改完要重启。

界面上其实早就有入口：「系统设置」页有一张「模型服务密钥 / 服务地址」表单。但它的保存按钮打向 `PUT /api/provider/configuration` —— **这个接口后端从未实现**，必然 404。也就是说这个表单从上线至今一直是坏的。

本设计要做的事：把这张表单真正实现出来，改成「视频生成」配置卡，填完保存即写库、即生效。

## 二、范围

**做**：视频生成一种能力（`video.generate` / `cookies.video.standard`）。

**不做**：文本、图片、视频理解、语音等其他能力的界面配置。接口设计上留出能力参数，以后扩展不用改结构，但本次不实现也不在界面上出现。

## 三、界面

「系统设置」页的模型服务区改成一张卡片：

```
视频生成                                    ● 已配置 · 服务端加密保存
─────────────────────────────────────────────────────
服务地址   [https://ark.cn-beijing.volces.com/api/v3    ]
密钥       [••••••••••  已保存，留空则不改动             ]
模型       [doubao-seedance-1-0-lite-t2v-250428    ▾]
─────────────────────────────────────────────────────
最近校验   2026-08-15 10:32 · 通过
                            [ 测试连接 ]  [ 保存并生效 ]
```

规则：

- **密钥留空 = 沿用已保存的那把**。改地址或换模型时不必重新粘密钥。已配置状态下密钥框不是必填。
- **模型是可输入的下拉框**，预置几个常用 Seedance 型号，也接受手输任意模型 ID。
- **保存即生效，不需要重启后端**。适配器每次提交任务时现查路由。
- 卡片头部显示当前生效来源：`服务端加密配置`（库里有）或 `部署环境配置`（回落到 .env）。

## 四、保存前的连通性探测

点「保存并生效」时先探一次，通过才写库；不通当场报错、不写库。「测试连接」按钮用同一套逻辑，只是不写库。

**探测方式（零成本）**：拿一个不存在的任务 ID 调 `GET {baseURL}/contents/generations/tasks/{探测ID}`。

| 上游响应 | 判定 | 界面文案 |
| --- | --- | --- |
| 401 / 403 | 密钥无效 | 密钥被拒绝，请确认是完整的 API Key |
| 404 / 400 | **通过** —— 密钥被接受，只是任务不存在 | 连接正常 |
| 连接失败 / 超时 / DNS 失败 | 地址不可达 | 服务地址连不上 |
| 5xx | 上游异常 | 模型服务暂时不可用，稍后重试 |

不产生任何视频任务，不产生费用。

**这次探测验不出模型名是否正确。** 验模型名必须真提交一次生成任务，那要花钱。模型名错会在第一次生成视频时暴露，届时把上游原话透传到任务失败信息里。这个边界要写在页面提示上，避免"测试通过"被理解成"一定能出片"。

## 五、数据落点

写入现有的五张 provider 表，与 PowerShell 脚本写的是同一处：

| 表 | 写什么 |
| --- | --- |
| `provider_connections` | 一条 `connection_type='ark'` 的连接，固定 code `ark-seedance` |
| `provider_connection_revisions` | 服务地址（每次改地址追加一个修订，不原地改） |
| `provider_credentials` | AES-GCM 密文 + nonce + 密钥版本。旧凭据置 `retired`，不删 |
| `provider_model_routes` | `capability=video.generate`，`model_alias=cookies.video.standard` |
| `provider_model_route_revisions` | 模型名 + 生成参数白名单（时长/比例/分辨率，用默认值，界面不暴露） |

界面要显示「最近校验时间 / 结果」，这需要持久化。新增一个迁移，给 `provider_connections` 加三列：`last_verified_at`、`last_verification_ok`、`last_verification_message`。探测成功和失败都记，刷新页面后仍在。

**为什么走现有表而不新建一张简单表**：视频适配器读库内路由那条路本来就是通的（脚本配完后跑的就是它），走现有表零改动即可生效；新建表反而要给适配器加第二条读取路径。而且以后扩展到文本、图片能力时，同一套接口换个 capability 参数即可。

**为什么不让界面去写 .env**：服务器上该文件不保证可写，且改完要重启，也不符合"写进数据库"的要求。

修订式写入（不原地改）是这几张表的既有约定，沿用它，顺带保留配置变更历史。

## 六、与 .env 直连模式的关系

两条路都保留，优先级：**库内配置 > .env 直连 > 未配置**。

判定必须发生在**每次提交任务时**，不能发生在启动装配时 —— 否则"保存即生效"做不到：服务启动时库里还没配置，装配成直连模式后，界面上存进去的配置要等重启才会被看见。

所以：只要视频适配器是 `ark_video`，就**始终**装配库内路由解析器，并给 Provider 服务加一个开关 `VideoRouteOptional`（在 `.env` 直连凭据齐全时为 true）。提交任务时：

- 库里查到启用路由 → 用库里的（`source=workspace`）
- 库里查不到，且 `VideoRouteOptional` → route 置空，适配器用 `.env` 里的凭据（`source=environment`）
- 库里查不到，且没有 env 兜底 → 报错，提示去设置页配置

页面显示当前实际生效的来源，避免"界面上改了却被 env 悄悄覆盖"。

`status` 判定同此顺序：库内有启用路由且有活跃凭据 → `configured` / `workspace`；否则 env 三项齐全 → `configured` / `environment`；否则 `not_configured`。

## 七、主密钥前置

库内加密需要 `COOKIES_PROVIDER_MASTER_KEY`（base64 的 32 字节），现在 `.env` 里是空的。

照米云的既有做法：`.env` / `.env.example` 里给一个本地默认值（米云那块就是这么做的，`COOKIES_MIYUN_MASTER_KEY` 有默认值）。使用者无需任何操作。

部署文档里补一条：生产环境必须换成自己生成的值。

**代价**：这把钥匙一旦更换，此前存入的密钥无法解密，需要在界面上重填一次。这是加密存储的固有性质，不是缺陷。界面在解密失败时应显示"配置需要重新填写"而不是"服务故障"。

## 八、接口

照米云连接设置的既有形状（`GET` / `PUT` / `POST :verify`）：

```
GET    /platform/v1/provider/video-configuration
PUT    /platform/v1/provider/video-configuration
POST   /platform/v1/provider/video-configuration:verify
```

响应体（三个接口同构，密钥只回掩码，永不回明文）：

```json
{
  "capability": "video.generate",
  "status": "configured",
  "source": "workspace",
  "base_url": "https://ark.cn-beijing.volces.com/api/v3",
  "model": "doubao-seedance-1-0-lite-t2v-250428",
  "masked_api_key": "ark-****cdef",
  "updated_at": "2026-08-15T02:31:00Z",
  "last_verified_at": "2026-08-15T02:31:00Z",
  "last_verification": { "ok": true, "message": "连接正常" }
}
```

`PUT` 请求体：`{ "base_url": "...", "api_key": "...", "model": "..." }`，`api_key` 可省略表示沿用已保存的。

**权限**：新增 scope `provider.configuration.write`（读用现有登录态即可，与 `provider/capabilities` 一致）。本地 `COOKIES_LOCAL_SCOPES` 加上它。

**幂等**：`PUT` 是配置写入不是任务创建，沿用乐观并发（`expected_version`）而非 Idempotency-Key，与米云连接一致。

## 九、分层

新代码落在既有分层里，不新增跨系统依赖：

- `internal/platform/provider/gateway_config_write.go` — 新增写入方法（`SaveVideoConfiguration` / `GetVideoConfiguration`），与只读的 `ListCapabilities` 同属 `MySQLGatewayConfigStore`
- `internal/platform/provider/video_probe.go` — 探测逻辑，只依赖 HTTP，不依赖 DB
- `internal/platform/httpserver/` — 三个 handler，接口经 `ProviderConfigurationReader` 扩展成读写接口
- `src/components/ModelSettingsPage.tsx` + `src/context/ModelConfigContext.tsx` — 界面与状态
- `src/data/api.ts` — 三个接口的调用，替换掉现在指向 404 的三个

## 十、测试

- **写入**：保存后能读回；改地址不带密钥时旧密钥仍可用；换密钥时旧凭据转 `retired` 而非被删；并发写入靠版本号拒绝覆盖
- **探测**：401/403、404、400、5xx、超时五种上游响应各自映射到正确判定
- **密钥安全**：三个接口的响应体、日志、审计记录中都不出现明文密钥（用一个断言扫响应全文）
- **回落**：库里没有启用路由时回落到 env；库里有时以库为准
- **解密失败**：主密钥变更后读配置返回"需要重新填写"而非 500
- **界面**：保存成功后状态变为已配置；密钥留空提交不清空已存密钥；探测失败时不写库

## 十一、明确不做

- 不做多组织隔离（当前 `organization_id` 为 NULL 的全局路由已够用）
- 不在界面暴露生成参数白名单（时长/比例/分辨率），用默认值
- 不做密钥轮换排期、不做历史配置回滚界面（数据层保留了修订，界面暂不呈现）
- 不做模型名有效性校验（成本原因，见第四节）
