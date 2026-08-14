# AI 原生效果广告：商品链接、渠道规格、视频模型与音频/文字控制调研

日期：2026-08-12
范围：只记录平台官方文档、官方开放平台、模型厂商文档和当前仓库代码能够确认的事实；无法从一手资料确认的内容明确标注。本文是产品与技术决策依据，不是已经实现能力的声明。

## 1. 先给结论

1. **当前 AI 原生广告只正式支持抖音商品链接和抖音创作配置。**代码只允许 `v.douyin.com`、`haohuo.jinritemai.com` 两个商品链接域名；需求只允许 `douyin + 9:16 + zh-CN + 15～30 秒`。
2. **商品链接来源与广告投放渠道不是一回事。**淘宝商品可以生成投向抖音、快手或 Meta 的广告；Shopify 商品也可以生成抖音广告。两者应分别选择，不应由商品链接自动决定投放平台。
3. 可扩展的商品来源很多，但稳定生产接入通常不是“匿名抓网页”，而是“识别链接/商品 ID + 官方 API + 商家或联盟授权”。公开页面抓取只能作为易失效的兜底。
4. **渠道不能直接等同于一个比例。**正确模型是“渠道 → 广告版位 → 规格预设”。例如 YouTube Shorts 用 9:16，而 YouTube 插播和腾讯视频前贴片通常需要 16:9。
5. 当前视频路由是 `doubao-seedance-2-0-fast-260128`。Cookies Provider 层通用支持 9:16、16:9、1:1 和单段 4～15 秒，但 AI 原生业务层当前仍锁定 9:16；整片 15～30 秒由多个短视频 Unit 生成后合成。
6. 当前 AI 原生视频 Unit 明确以 `silent` 请求模型；旁白、字幕、BGM/音效属于后期时间线。这个架构反而适合新增用户开关，不需要让模型直接决定成片有没有旁白或字幕。
7. 用户需求不能只做一个“声音开关”。至少应拆成三条互相独立的轴：**旁白、字幕、画面卖点叠字**。BGM/音效可作为第四条轴。字幕不是“所有文字”，画面卖点叠字也不是字幕。

## 2. 当前实现的事实基线

### 2.1 商品链接

当前解析器位于 `internal/integrations/productsource/douyin.go`：

- 只允许 HTTPS；
- 只允许 `v.douyin.com` 和 `haohuo.jinritemai.com`；
- 支持分享文本中提取链接、跟随最多 5 次跳转、解析嵌套 `detail_schema`、归一化商品 ID；
- 从 `goods_detail` 读取标题、价格、销量和一张主图；
- 图片仅信任抖音电商 CDN 域名；
- 当前是分享页/落地 URL 解析，不是抖店商家授权 API。

因此当前应对用户表述为“已识别抖音商品链接”，不能表述为“已接入所有抖音店铺商品数据”。

### 2.2 渠道与规格

`internal/systems/creative/ai_native_requirement.go` 当前强制：

| 字段 | 当前值 |
| --- | --- |
| 渠道 | 仅 `douyin` |
| 比例 | 仅 `9:16` |
| 成片时长 | 15～30 秒，默认 20 秒 |
| 语言 | 仅 `zh-CN` |

`internal/platform/provider/video.go` 的 Provider 通用输入允许：

- 比例：9:16、16:9、1:1；
- 单次模型时长：4～15 秒；
- 分辨率：480p、720p、1080p（但当前 Ark 适配器主动拒绝 1080p）。

当前数据库路由（本次调研时的本地配置）为：

```text
cookies.video.standard -> doubao-seedance-2-0-fast-260128
```

路由声明支持 9:16、16:9、1:1，480p/720p、单段 4～15 秒。这里是**本地路由契约**，不是厂商对所有账号、所有模型版本的永久承诺。

### 2.3 音频

`AINativeGenerationUnit.ProviderInput` 当前固定 `AudioPolicy: silent`。随后系统根据故事板的 `Voiceover` 创建独立 Speech Unit，再由 FFmpeg 时间线完成旁白、字幕、BGM/音效和画面合成。

这意味着：

- 当前视频画面生成模型本身不承担最终旁白；
- “不要旁白”应跳过 TTS 与旁白轨，不必换视频模型；
- “不要字幕”应跳过字幕轨，不应删除故事板中的语义文本；
- “不要字幕但保留卖点文字”是完全合理且必须支持的组合。

## 3. 商品链接可以扩展到哪些平台

### 3.1 推荐统一接入方式

每个平台都使用同一条产品契约：

```text
识别平台与 URL
  → URL 归一化、提取商品 ID
  → 优先调用官方 API（获得商家/联盟授权时）
  → 未授权时只返回可证实的有限公开信息
  → 不足部分由用户上传商品图或手工确认
```

建议状态必须区分：

| 状态 | 含义 |
| --- | --- |
| `recognized` | 只识别了平台和商品 ID |
| `authorized_fetched` | 已通过官方授权 API 获取商品快照 |
| `partial_public_fallback` | 只取得有限公开信息，随时可能失效 |
| `requires_merchant_authorization` | 需要商家/店铺授权才能继续 |
| `unsupported` | 当前不支持 |

“识别成功”不能暗示“完整商详已抓取成功”。

### 3.2 平台能力与现实边界

| 商品来源 | 官方可持续接入 | 可获得的典型数据 | 授权与边界 | 建议优先级 |
| --- | --- | --- | --- | --- |
| 抖音电商/抖店 | 抖店开放平台商品详情、商品列表、SKU API | 商品标题、主图/详情、SKU、价格、库存等，具体以权限包为准 | 商品 API 明确需要店铺授权；匿名分享链接不是官方稳定商详接口 | 已有链接识别；下一步补正式授权 API |
| 淘宝/天猫 | 淘宝开放平台商品 API | 商品描述、价格、SKU、库存、图片、店铺等 | 获取商家/用户商品数据需淘宝 OAuth/SessionKey；淘口令和页面抓取不稳定 | 第二批 |
| 京东 | 京东联盟/JOS 商品查询 | SKU 名称、主图、类目、价格、物流、自营标识、推广信息等 | 需京东联盟/JOS 应用及接口权限；普通公开页面不等于官方 API | 第二批，尤其适合广告推广场景 |
| 拼多多 | 官方开放平台存在 | 本次公开一手资料不足以确认当前商品详情接口字段与准入 | 必须先在官方控制台确认应用类型、商品接口和授权；不能用第三方 SDK 文档代替官方承诺 | 准入核验后再排期 |
| 快手电商 | 快手电商开放平台商品管理 | 商品与 SKU、上下架、库存等 | 自用型/第三方应用，需注册认证和店铺授权；匿名分享链接不稳定 | 第三批 |
| 小红书 | 小红书 Ark 开放平台商品接口 | 商品结构、名称、品牌/类目、图片、描述、特色、规格、价格、库存/状态 | 面向合作商家/第三方伙伴账户内商品；不能把任意笔记链接当商品详情 API | 第三批，需商务/资质 |
| Amazon | SP-API Catalog Items 或 Creators API | ASIN、标题、图片、属性、分类、报价/推广信息（视 API 与权限） | SP-API 需卖家/供应商 OAuth；Creators API 需联盟资格。PA-API 已宣告弃用 | 海外批次，准入较重 |
| Shopify | Storefront GraphQL API | 标题、描述、媒体、价格、变体、可售状态 | 标准 `/products/{handle}` 易识别；部分基础商品查询可 tokenless，但并非任意店铺都开放，稳定方案仍是店主授权/Storefront token | 第二批，技术最通用 |

### 3.3 官方来源

抖音电商：

- [抖店开放平台商品 API 目录](https://op.jinritemai.com/docs/api-docs/14)
- [商品列表 listV2：明确需店铺授权](https://op.jinritemai.com/docs/api-docs/14/633)
- [SKU 详情：授权与返回字段](https://op.jinritemai.com/docs/api-docs/14/104)
- [抖店开放平台 API 文档入口](https://op.jinritemai.com/docs/api-docs/)

淘宝/天猫：

- [淘宝开放平台 Web 授权说明](https://developer.alibaba.com/docs/doc.htm.htm?articleId=102635&docType=1&treeId=1)
- [淘宝开放平台商品 API 目录](https://developer.alibaba.com/docs/api.htm?apiId=6)
- [商品详情响应字段示例](https://developer.alibaba.com/docs/api.htm?apiId=23559)

京东：

- [京东联盟开放平台与商品接口](https://jos.jd.com/jdunion)
- [JOS OAuth2 与开放平台介绍](https://jos.jd.com/doc/channel.htm?id=808)
- [京东 API 签名/公共参数工具](https://jos.jd.com/commontools?id=2)

拼多多：

- [拼多多开放平台](https://open.yangkeduo.com/)

快手：

- [快手电商开放平台](https://open.kwaixiaodian.com/)
- [商品接口入口示例](https://open.kwaixiaodian.com/docs/api?apiName=open.item.new&version=1)
- [快手开放平台挂载商品 ID 文档](https://open.kuaishou.com/platform/openApi?menu=20)

小红书：

- [小红书 Ark 开放平台简介](https://school.xiaohongshu.com/en/open/quick-start/introduction.html)
- [商品详情查询](https://school.xiaohongshu.com/en/open/product/product-detail.html)
- [商品列表与字段](https://school.xiaohongshu.com/en/open/product/item-list.html)

Amazon：

- [Amazon SP-API Catalog Items](https://developer-docs.amazon.com/sp-api/lang-en_US/reference/catalog-items-v2022-04-01)
- [Amazon 公共应用由 Selling Partner 授权](https://developer-docs.amazon.com/sp-api/lang-US/docs/authorize-public-applications)
- [Amazon Creators API](https://affiliate-program.amazon.com/creatorsapi/docs/en-us/introduction)
- [PA-API 迁移/弃用说明](https://affiliate-program.amazon.com/creatorsapi/docs/en-us/paapiv5-deprecation)

Shopify：

- [Shopify Storefront API 2026-04](https://shopify.dev/docs/api/storefront/2026-04)
- [Shopify Storefront Product 对象](https://shopify.dev/docs/api/storefront/latest/objects/Product)
- [Storefront API 店铺侧接入](https://shopify.dev/docs/storefronts/headless/building-with-the-storefront-api/getting-started)

## 4. 主流投放渠道：比例与时长不是一张单值表

### 4.1 第一版可落地的版位预设

| 渠道/版位预设 | 建议主比例 | 第一版产品时长 | 当前模型管线能否产出 | 备注 |
| --- | --- | --- | --- | --- |
| 抖音/巨量信息流 | 9:16 | 15 或 20/30 秒 | 能；当前 AI 原生正是 9:16、15～30 秒 | 需要继续按具体巨量创意规格接口校验，而不是写死全平台唯一规格 |
| 快手信息流 | 9:16 | 15 或 30 秒 | 能 | 上线前通过磁力引擎账户/创意规格 API 确认具体版位 |
| 视频号原生短视频 | 9:16 | 15 或 30 秒 | 能 | 官方资料显示视频号竞价外层视频允许范围很宽；产品应给短视频效果广告预设，不照搬上限 |
| 腾讯视频前贴片 | 16:9 | 15 秒 | Provider 能；AI 原生业务层暂不能 | 这是典型“同属腾讯广告但不是 9:16”的反例 |
| TikTok In-Feed | 9:16（推荐） | 15～30 秒 | 能 | 官方政策允许 9:16/1:1/16:9、5～60 秒；部分地区/样式要求音频，静态画面不能占主导 |
| Instagram/Facebook Reels | 9:16 | 15～30 秒 | 能 | Reels 是全屏竖版；Meta 强调声音和安全区，但可接受的 Reels 上传比例范围更宽 |
| YouTube Shorts | 9:16 | 15～30 秒 | 能 | 官方规格建议 9:16；Shorts 资产建议 6～60 秒 |
| YouTube In-stream | 16:9 | 6 秒 bumper、15/30 秒插播 | Provider 能；AI 原生业务层暂不能 | 6 秒 Bumper 还低于当前整片 15 秒下限，需要新流程预设 |

### 4.2 一手来源能确认的关键规则

- Google Ads 建议一个广告组同时提供 16:9、9:16、1:1 三种方向，而不是只交一条万能视频；YouTube 视频广告规格建议 16:9/1920×1080、9:16/1080×1920、1:1/1080×1080。Bumper 为 6 秒；不可跳过插播最长通常 30 秒；Shorts 至少 5 秒，规格页建议 6～60 秒。[Google 创意指导](https://support.google.com/google-ads/answer/13812351?hl=en) [Google 视频广告规格](https://support.google.com/google-ads/answer/17091270?hl=en-AU) [创建视频广告系列](https://support.google.com/google-ads/answer/2375497?hl=en)
- TikTok 信息流官方政策列出 9:16、1:1、16:9 和 5～60 秒，并要求广告有音频、画面动态、清晰可读；TikTok 竞价信息流推荐 9:16，最低 540×960，并提供安全区文件。不同地区和广告产品可能另有规则。[TikTok 广告样式政策](https://ads.tiktok.com/help/article/tiktok-ads-policy-ad-format-and-functionality?lang=en&redirected=2) [TikTok 竞价信息流规格](https://ads.tiktok.com/help/article/tiktok-auction-in-feed-ads?lang=zh&redirected=2)
- Instagram Reels 可上传 1.91:1 到 9:16，最低 720p/30fps；Reels 广告应为全屏竖版，可最长 15 分钟。Meta 的 Reels 业务指南推荐 9:16、声音开启且关键元素处于安全区。[Instagram Reels 比例](https://www.facebook.com/help/1038071743007909) [Instagram Reels 广告](https://www.facebook.com/help/instagram/546362593027755) [Meta Reels 广告指南](https://www.facebook.com/business/ads/facebook-instagram-reels-ads)
- 巨量引擎公开帮助页能确认开屏广告的静态/动态/视频秒数，但公开页面不足以作为所有抖音竞价信息流的完整规格矩阵。生产接入应调用/查询具体账户与版位的创意规格，而非凭经验写死。[巨量引擎广告展现帮助](https://www.oceanengine.com/help/guanggao-zhanxian)
- 腾讯广告开放 API 提供“获取创意规格详情”，返回支持的文件格式、最大时长等，说明规格应按创意规格 ID 动态读取。腾讯官方培训资料显示腾讯视频前贴片为 16:9、竞价约 14～15 秒；视频号原生广告官方资料显示外层视频允许 5～90 秒（竞价）。[腾讯广告获取创意规格详情](https://s.apifox.cn/apidoc/docs-site/3515798/api-123287552) [腾讯视频类广告位素材参数](https://training.tencentads.com/uploads/202104/uGVGnF6w_3IHxrl.pdf) [视频号原生广告竞价推广能力](https://training.tencentads.com/uploads/202306/iiP4sj9B_EQId5g.pdf)
- 本次公开检索没有找到能够完整、稳定呈现快手磁力引擎各版位精确比例/时长矩阵的一手公开页面。因此只能把 9:16/15～30 秒当作第一版产品预设，不能写成“快手全部广告的官方唯一规格”；正式开发前应在磁力引擎广告账户或官方创意规格接口做逐版位核验。

### 4.3 产品建模建议

不要这样建：

```text
channel = douyin → ratio = 9:16
```

应当这样建：

```text
source_platform = taobao | douyin_shop | shopify | ...
delivery_channel = douyin | kuaishou | wechat_channels | meta | youtube | tiktok
placement = feed | reels | shorts | in_stream | pre_roll | ...
output_preset = ratio + resolution + total_duration + safe_zone + audio_rule
```

第一期可先提供三个真实可生产的预设：

1. `vertical_social_15s`：9:16、720p、15 秒；
2. `vertical_social_30s`：9:16、720p、30 秒；
3. `landscape_preroll_15s`：16:9、720p、15 秒。

但第三项在 AI 原生业务层尚未开放，必须先解除 `douyin/9:16` 验证与补充相应渠道 Profile。

## 5. Seedance 模型是否支持这些规格

### 5.1 能够确认的模型事实

火山方舟官方信息确认 Seedance 2.0：

- 是音视频联合生成架构；
- 支持文本、图片、音频、视频四模态输入；
- 最多可参考 9 张图片、3 段视频、3 段音频；
- 单次最长生成 15 秒；
- 官方资源包页面列出标准版 480p/720p/1080p、4～15 秒。

来源：[Seedance 2.0 官方发布](https://developer.volcengine.com/articles/7606009619928449070) [Seedance 2.0 官方资源包页](https://www.volcengine.com/activity/seedance2) [Seedance 2.0 提示词指南](https://www.volcengine.com/docs/82379/2222480?lang=zh)

Seedance 1.5 Pro 官方产品材料确认其支持有声视频和音画同步；官方视频生成示例/工具列出 9:16、16:9、1:1、3:4、4:3、21:9 等比例、2～12 秒及 `generate_audio`。这些材料能够说明 1.5 Pro 的产品/官方示例能力，但精确生产参数仍应以当前账号 API Schema 和最小能力探测为准。[Seedance 1.5 Pro 提示词指南](https://www.volcengine.com/docs/82379/2168087?lang=zh) [火山引擎官方开发者社区视频生成工具](https://developer.volcengine.com/articles/7615547765435432996)

### 5.2 当前项目应该怎样解释“模型支持”

| 问题 | 判断 |
| --- | --- |
| 能否做 9:16、16:9、1:1？ | Provider 层和当前路由声明可以；AI 原生业务当前只开放 9:16 |
| 能否做 15～30 秒整片？ | 可以，但不是一次模型调用：拆成多段 4～15 秒 Unit 再合成 |
| 能否做 1080p？ | Seedance 2.0 标准版官方材料列出 1080p；但当前项目适配器和路由只放行 480p/720p，当前不可宣称已支持 1080p |
| 能否模型原生生成声音？ | Seedance 2.0/1.5 Pro 具备有声能力；但当前 AI 原生管线明确请求静音 Unit，最终声音由独立后期管线生成 |
| 能否让模型正确生成画面中文字？ | 不应依赖。即使模型有文字响应能力，商品卖点、价格、免责声明、字幕仍应由后期确定性排版合成，保证正确、可编辑、可审计 |
| 一条视频能否直接覆盖所有渠道？ | 不能。应从一个故事板生成多个“渠道版位变体”，分别应用比例、安全区、时长与声音规则 |

特别注意：截至本次调研，Seedance 2.0 标准版的规格资料比 Fast 完整；不能自动假设 `doubao-seedance-2-0-fast-260128` 与标准版在 1080p、所有比例、音频和多模态组合上完全一致。当前能依赖的是本地路由声明和已通过的能力探测。

## 6. 用户到底该怎样选择旁白、字幕与文字卖点

### 6.1 先把三个概念分开

| 内容 | 是什么 | 是否跟着声音逐句变化 | 示例 |
| --- | --- | --- | --- |
| 旁白 Voice-over | 一条人声轨，讲述广告文案 | 是声音本身 | “通勤出门，一包装下整天所需” |
| 字幕 Captions | 旁白/对白的逐字或逐句转写，主要为静音观看和无障碍 | 通常是 | 屏幕下方同步显示旁白内容 |
| 卖点叠字 Sales overlays | 画面中的信息卡、标题、价格、功能标签、CTA | 不一定 | “3D 透气背板”“可扩容”“立即购买” |

所以以下组合都合理：

| 旁白 | 字幕 | 卖点叠字 | 适用场景 |
| --- | --- | --- | --- |
| 有 | 有 | 有 | 默认效果广告成片，兼顾声音开启与静音观看 |
| 有 | 无 | 有 | 画面更简洁，但静音观看损失叙事；适合用户自行后期加字幕 |
| 无 | 无意义/关闭 | 有 | 纯演示广告、产品视觉片，用户后期加配音；应保留核心卖点与 CTA |
| 无 | 有（编辑型文案） | 有 | 无人声的文字叙事/信息流演示；此时应叫“叙事字幕”而不是语音字幕 |
| 有 | 有 | 无 | 品牌感较强、少贴纸化；但关键商品事实可能只靠口播，不一定适合转化广告 |
| 无 | 无 | 无 | 纯净画面素材/后期母版；应明确提示“当前不是可直接投放的完整效果广告” |

“有字幕、无旁白”不能简单由 TTS 转写生成，因为根本没有音频。系统需要把脚本中的字幕/叙事文案作为独立时间线渲染。

### 6.2 推荐的用户配置模型

不要使用含糊的“是否生成声音”和“是否生成文字”两个开关。建议配置为：

```text
交付模式
├─ 完整广告（推荐）
│  ├─ 旁白：开
│  ├─ 字幕：跟随旁白
│  ├─ 卖点叠字：开
│  └─ BGM/音效：开
├─ 无旁白成片
│  ├─ 旁白：关
│  ├─ 字幕：叙事字幕 / 关闭（二选一）
│  ├─ 卖点叠字：开
│  └─ BGM/音效：开
└─ 纯净视频素材
   ├─ 旁白：关
   ├─ 字幕：关
   ├─ 卖点叠字：关（可由用户改开）
   └─ BGM/音效：关（可由用户改开）
```

“高级设置”再允许单独控制四项：

```json
{
  "voiceover_mode": "generated | none",
  "caption_mode": "from_voiceover | editorial | none",
  "sales_overlay_mode": "key_points | minimal | none",
  "music_sfx_mode": "generated | none"
}
```

约束：

- `caption_mode=from_voiceover` 必须要求 `voiceover_mode=generated`；
- 无旁白但要文字叙事时使用 `caption_mode=editorial`；
- “用户后期处理”最好选择纯净视频素材，同时保留可下载的脚本、字幕草稿/SRT 和时间线 JSON，便于后期，而不是销毁这些内容；
- 价格、促销、功效、免责声明和 CTA 属于卖点/合规叠字，不应由“字幕关闭”顺带删除；
- TikTok 某些广告规则要求音频，因此选择“全静音”时要提示该渠道/版位可能不可直接投放；平台规格应在导出时二次校验。

### 6.3 应该在哪个步骤选择

**主选择放在第一步“需求分析/生成要求”。**原因是它会影响脚本和故事板：

- 无旁白脚本不应硬写满口播句子；
- 无字幕时故事板要更依赖视觉动作和卖点卡；
- 无叠字时商品卖点必须由画面证明，而不是只存在脚本文字里；
- 纯净素材模式要避免让模型直接在画面中生成可见文字。

推荐位置：视频比例、时长、语言下面增加“交付模式”，默认“完整广告（旁白 + 字幕 + 卖点文字）”。用户选预设后，可以展开高级设置。

**第二个确认点放在故事板页面。**这里不重新定义需求，只允许查看和微调：

- 每个分镜旁白；
- 对应字幕/叙事字幕；
- 卖点叠字内容、出现时间和安全区；
- BGM/音效方向。

如果用户在故事板阶段改变全局模式，应明确提示会重新计算/作废声音与文字相关的下游时间线，但不必让需求分析和商品素材全部作废。

**视频生成页只显示即将生产的轨道并允许最终确认，不宜第一次询问。**例如：

```text
本次将生成：画面 ✓  旁白 ×  字幕 ×  卖点叠字 ✓  BGM/音效 ✓
```

视频生成后可提供“导出变体”，例如同一画面导出完整版和纯净版；前提是合成管线保留干净画面 Unit 与分轨资产。

## 7. 建议的产品决策

### P0：现在最值得做

1. 保持商品来源仍为抖音，但把 UI 文案明确成“商品来源：抖音电商”。
2. 在需求分析增加三种交付模式和高级四轴设置。
3. 默认使用“完整广告”：旁白开、字幕跟随旁白、卖点叠字开、BGM/音效开。
4. 支持“纯净视频素材”，并保留脚本、字幕草稿和时间线供下载，不生成/不混入音频、字幕和叠字。
5. 将卖点文字确定性地后期渲染，禁止让视频模型直接生成价格、参数、Logo 或免责声明。
6. 在故事板中分别展示旁白、字幕和卖点叠字，不能继续混成一个 `voiceover/subtitle` 概念。

### P1：渠道扩展

1. 将“商品来源”与“投放渠道/版位”分离建模。
2. 先开放 `vertical_social_15s` 和 `vertical_social_30s`，对应抖音、快手、视频号、TikTok、Reels、Shorts 的渠道 Profile。
3. 每个 Profile 包含脚本节奏、安全区、CTA、音频要求和输出规格，而不仅是一段提示词。
4. 再开放 `landscape_preroll_15s`，覆盖腾讯视频/YouTube 横版场景。
5. 导出前按目标渠道版位校验，不把“能生成”误认为“能投放”。

### P2：商品来源扩展

优先顺序建议：

1. Shopify；
2. 京东联盟；
3. 淘宝/天猫商家授权；
4. Amazon；
5. 快手、小红书；
6. 拼多多在官方控制台准入核验后再决定。

每增加一个平台都应实现一个独立 `ProductSourceAdapter`，输出统一 `ProductSnapshot`，而不是把不同站点的页面解析继续堆进 `DouyinResolver`。

## 8. 仍需账号内验证的项目

1. `doubao-seedance-2-0-fast-260128` 在当前账号对 16:9、1:1、各时长、`generate_audio` 的实际支持组合；
2. Fast 是否支持 1080p；当前项目明确不支持；
3. 巨量、快手、腾讯各目标广告账户中具体版位的最新创意规格；
4. TikTok “必须有音频”的规则对计划投放国家和具体广告产品是否适用；
5. 各电商平台应用资质、可申请权限包和商家授权范围；
6. “纯净素材”是否需要同时交付 SRT、旁白 WAV、无字幕 MP4、带透明通道文字层或时间线 JSON，应由实际后期团队确认。

## 9. 最重要的产品语言

- 不说“我们支持所有电商链接”，而说“支持识别的平台 + 已授权获取的平台”。
- 不说“选择抖音就固定 9:16”，而说“选择抖音信息流版位，系统应用 9:16 规格预设”。
- 不把“字幕关闭”理解为“所有画面文字消失”。
- 不把“没有旁白”理解为“视频必须完全静音”；BGM/音效是独立选择。
- 不把“视频模型支持某比例”理解为“当前 AI 原生产品已经开放该比例”。
- 不把“视频生成成功”理解为“素材一定符合目标平台投放规范”；导出前仍需规格与合规校验。
