#!/usr/bin/env bash
# 把「投前」那一屏铺满，专供录屏演示。
#
# 前置：先跑 scripts/seed-insight-showcase.sh 建好项目和那份已确认的复盘。
# 这个脚本只做一件事——往同一份复盘上再沉淀几条经验，让投前的三样东西都有内容：
#
#   结论列表   —— 6 条能照着做的（状态在用 + 判定能归因），够翻两屏
#   三个筛选   —— 渠道/广告类型/目标各有 3 个以上取值，选下拉时是真的在筛
#   历史模式   —— hook_type 被 3 条独立结论提到，另有 4 个特征各被 2 条提到
#
# 顺带把投前那几条边界提示也铺出来，录的时候能现场演示：
#   跨渠道提示 —— 结论横跨抖音/视频号/小红书，不勾「跨渠道一起看」就会出警告
#   没写适用范围 —— 特意留一条空适用范围的，选了渠道之后会报「另有 1 条被挡在外面」
#   👁 只是观察 —— showcase 里那条 directional 的经验进不来，这是设计如此
#
# 可重复执行：按结论正文查重，跑第二遍只会打印「已存在」。
#
# 用法：
#   bash scripts/seed-insight-prelaunch-demo.sh
#   COOKIES_API=http://14.103.24.58:8091 bash scripts/seed-insight-prelaunch-demo.sh
set -euo pipefail

# Windows 上 python 默认按 gbk 写 stdout，中文结论会直接炸在编码上。
export PYTHONUTF8=1 PYTHONIOENCODING=utf-8

BASE="${COOKIES_API:-http://127.0.0.1:8080}"
PROJECT_NAME="${COOKIES_DEMO_PROJECT_NAME:-Nova 家清 · 夏季清洁新品投放}"
USERNAME="${COOKIES_ADMIN_USERNAME:-Admin}"
PASSWORD="${COOKIES_ADMIN_PASSWORD:-123456}"

python - "$BASE" "$PROJECT_NAME" "$USERNAME" "$PASSWORD" <<'PY'
import http.cookiejar
import json
import sys
import urllib.error
import urllib.request

BASE, PROJECT_NAME, USERNAME, PASSWORD = sys.argv[1:5]

opener = urllib.request.build_opener(
    urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))


def call(method, url, body=None, tolerate=()):
    data = json.dumps(body, ensure_ascii=False).encode("utf-8") if body is not None else None
    request = urllib.request.Request(url, data=data, method=method)
    if data is not None:
        request.add_header("Content-Type", "application/json")
        request.add_header("Idempotency-Key", f"seed-prelaunch-{abs(hash(url + str(data))):x}")
    try:
        with opener.open(request) as response:
            raw = response.read()
    except urllib.error.HTTPError as err:
        if err.code in tolerate:
            return None
        raise SystemExit(f"{method} {url} -> {err.code} {err.read().decode('utf-8', 'replace')}")
    return json.loads(raw) if raw else {}


call("POST", f"{BASE}/platform/v1/auth/login", {"username": USERNAME, "password": PASSWORD})

project = next((item for item in call("GET", f"{BASE}/platform/v1/projects")["items"]
                if item["name"] == PROJECT_NAME), None)
if project is None:
    raise SystemExit(f"没找到项目「{PROJECT_NAME}」。先跑 scripts/seed-insight-showcase.sh。")
PROJECT = project["id"]
ROOT = f"{BASE}/api/insights/v1/projects/{PROJECT}"

# 经验必须挂在一份已确认的复盘上：投前那一屏的每条结论都要能点开「凭什么」，
# 而「凭什么」追的就是这份复盘。挂在草稿上的经验，证据链断在半路。
reports = call("GET", f"{ROOT}/reports?limit=50")["items"]
report = next((item for item in reports if item["status"] == "confirmed"), None)
if report is None:
    raise SystemExit("这个项目还没有已确认的复盘。先跑 scripts/seed-insight-showcase.sh。")
print(f"挂到复盘 {report['id']}")

# 特征键取自 internal/systems/insights/features.go 的字段表，不是随手编的字符串：
# 「历史模式」按这个键聚合，编一个表里没有的键，聚出来的模式对不上任何素材字段。
#   hook_type        钩子类型      —— 故意让 3 条结论都提到，它是模式榜首
#   cta / proof      CTA / 证明    —— 各 2 条
#   subtitle_density 字幕密度      —— 2 条
#   duration         时长          —— 2 条
EXPERIENCES = [
    {
        "conclusion": "数字人口播的字幕密度压到每屏 12 字以内时，3 秒完播率提升约 6 个百分点；密度越高完播越差，超过 20 字反而不如无字幕。",
        "conditions": ["抖音信息流", "数字人口播", "竖版 9:16", "单条曝光 ≥ 2 万"],
        "counterexamples": ["静音场景下不适用：没有字幕的素材在静音里等于没信息。"],
        "card_type": "statistic",
        "confidence": "sufficient",
        "recommended_action": "脚本阶段就把每屏字数写进要求，别等剪辑再压。",
        "applicability": {
            "brands": ["Nova 家清"], "channels": ["抖音"],
            "creative_types": ["数字人口播"], "objectives": ["新品拉新"],
            "time_range_note": "夏季清洁旺季（6—8 月）",
        },
        "content_basis": {"features": ["hook_type", "subtitle_density"],
                          "note": "同一批数字人素材，只有字幕密度这一个变量不同。"},
        "data_basis": {"asset_count": 6, "sample_size": 412300,
                       "metrics": ["completion_rate_3s", "ctr"],
                       "baseline": "高字幕密度组（每屏 ≥ 20 字）"},
    },
    {
        "conclusion": "真人口播里放一段第三方检测报告的特写，比只念卖点的转化率高约 0.9 个百分点；把 CTA 提到 15 秒之前，这个差距还能再拉开。",
        "conditions": ["抖音信息流", "真人口播", "客单价 ≥ 79 元", "单条曝光 ≥ 1 万"],
        "counterexamples": ["低客单价（≤ 39 元）没跑出差异：便宜到不需要说服。"],
        "card_type": "statistic",
        "confidence": "sufficient",
        "recommended_action": "客单价 79 元以上的品，脚本里必须留一段证明镜头，CTA 压在 15 秒前。",
        "applicability": {
            "brands": ["Nova 家清"], "channels": ["抖音"],
            "creative_types": ["真人口播"], "objectives": ["转化"],
        },
        "content_basis": {"features": ["proof", "cta"],
                          "note": "证明镜头有无 × CTA 前后两组，2×2 分组比出来的。"},
        "data_basis": {"asset_count": 8, "sample_size": 536800,
                       "metrics": ["cvr", "cpa"], "baseline": "无证明镜头组"},
    },
    {
        "conclusion": "视频号的数字人口播控制在 22 秒以内时，完播率明显更高；同一批素材剪到 35 秒，完播率掉一半，但抖音上没有这个断点。",
        "conditions": ["视频号", "数字人口播", "单条曝光 ≥ 1 万"],
        "counterexamples": ["抖音不适用：抖音同批素材 35 秒的完播率没有明显下降。"],
        "card_type": "fact",
        "confidence": "sufficient",
        "recommended_action": "投视频号的版本单独剪一版 22 秒的，别直接搬抖音的长版。",
        "applicability": {
            "brands": ["Nova 家清"], "channels": ["视频号"],
            "creative_types": ["数字人口播"], "objectives": ["新品拉新"],
        },
        "content_basis": {"features": ["hook_type", "duration"],
                          "note": "同一批素材两个时长版本，钩子不变。"},
        "data_basis": {"asset_count": 5, "sample_size": 188400,
                       "metrics": ["completion_rate", "ctr"], "baseline": "35 秒版本"},
    },
    {
        "conclusion": "小红书图文的封面只放「使用后」的对比图，收藏率比放产品正面图高约 2.3 个百分点；封面上带字的版本没有额外增益。",
        "conditions": ["小红书", "图文笔记", "封面为实拍", "单篇曝光 ≥ 5000"],
        "counterexamples": [],
        "card_type": "statistic",
        "confidence": "sufficient",
        "recommended_action": "小红书这条线的封面统一按「使用后对比」出，正面图只留一版做对照。",
        "applicability": {
            "brands": ["Nova 家清"], "channels": ["小红书"],
            "creative_types": ["图文笔记"], "objectives": ["种草"],
        },
        "content_basis": {"features": ["cover", "subtitle_density"],
                          "note": "封面形式三组，其余排版保持一致。"},
        "data_basis": {"asset_count": 9, "sample_size": 96500,
                       "metrics": ["save_rate", "ctr"], "baseline": "产品正面图封面组"},
    },
    {
        "conclusion": "把「限时立减」写进 CTA 口播、而不是只压在贴片上时，转化成本低约 11%；但连续投到第二周这个增益就消失了。",
        "conditions": ["抖音信息流", "数字人口播", "有明确优惠机制", "投放 ≤ 7 天"],
        "counterexamples": ["常销期不适用：没有真优惠时这么念，转化成本反而更高。"],
        "card_type": "statistic",
        "confidence": "sufficient",
        "recommended_action": "只在有真优惠的短周期用；第二周开始换回卖点型 CTA。",
        "applicability": {
            "brands": ["Nova 家清"], "channels": ["抖音"],
            "creative_types": ["数字人口播"], "objectives": ["转化"],
        },
        "content_basis": {"features": ["cta", "proof"],
                          "note": "优惠是否入口播两组，同一批数字人素材。"},
        "data_basis": {"asset_count": 6, "sample_size": 344700,
                       "metrics": ["cpa", "cvr"], "baseline": "优惠只压贴片组"},
    },
    # 特意留一条空适用范围：它证明不了自己适用于本次投放，所以一旦在页面上选了
    # 渠道/广告类型/目标，投前就会把它挡掉并报「另有 1 条没写适用范围」。
    # 录屏时这一条是用来演示「被挡掉的不静默丢」的，不是漏填。
    {
        "conclusion": "同一条素材的第二个剪辑版本，如果只换了背景音乐，投放表现和第一版没有统计意义上的差别。",
        "conditions": ["仅更换背景音乐", "其余变量不变"],
        "counterexamples": [],
        "card_type": "statistic",
        "confidence": "sufficient",
        "recommended_action": "别为了换 BGM 单开一版排期，把产能留给换钩子。",
        "applicability": {},
        "content_basis": {"features": ["duration"], "note": "只改 BGM 的对照组。"},
        "data_basis": {"asset_count": 4, "sample_size": 127600,
                       "metrics": ["ctr", "cvr"], "baseline": "第一版剪辑"},
    },
]

deposited = {item["conclusion"] for item in call("GET", f"{ROOT}/experiences?limit=200")["items"]}
added = 0
for spec in EXPERIENCES:
    if spec["conclusion"] in deposited:
        print(f"已存在，跳过：{spec['conclusion'][:24]}…")
        continue
    # 每条都重取一次复盘版本：沉淀经验会碰报告，拿旧版本号提交会被乐观锁挡回来。
    fresh = next(item for item in call("GET", f"{ROOT}/reports?limit=50")["items"]
                 if item["id"] == report["id"])
    created = call("POST", f"{ROOT}/reports/{report['id']}:create-experience", {
        "expected_report_version": fresh["version"],
        "conclusion": spec["conclusion"],
        "conditions": spec["conditions"],
        "counterexamples": spec["counterexamples"],
        "card_type": spec["card_type"],
        "confidence": spec["confidence"],
        "recommended_action": spec["recommended_action"],
        "applicability": spec["applicability"],
        "content_basis": spec["content_basis"],
        "data_basis": spec["data_basis"],
    })
    # 不确认就到不了投前：投前只放行「状态在用 + 判定能归因」，
    # 沉淀出来默认是「待确认」，差这一步。
    call("POST", f"{ROOT}/experiences/{created['id']}:confirm",
         {"expected_version": created["version"]})
    added += 1
    print(f"沉淀并确认 {created['id']}：{spec['conclusion'][:24]}…")

result = call("GET", f"{ROOT}/prelaunch")
print(f"\n新增 {added} 条。现在投前这一屏：")
print(f"  结论      {len(result['cards'])} 条")
print(f"  历史模式  {len(result['patterns'])} 个特征 —— "
      + "、".join(f"{p['feature']}×{p['card_count']}" for p in result["patterns"]))
print(f"  渠道      {'、'.join(result['facets']['channels']) or '（无）'}")
print(f"  广告类型  {'、'.join(result['facets']['creative_types']) or '（无）'}")
print(f"  目标      {'、'.join(result['facets']['objectives']) or '（无）'}")
print(f"  跨渠道    {'、'.join(result['mixed_channels'])}（不勾「跨渠道一起看」会出提示）")
print(f"\n项目 ID：{PROJECT}")
PY
