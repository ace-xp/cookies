#!/usr/bin/env bash
# 从零铺一个可以直接拿去演示的素材洞察项目：建项目 → 接数据源 → 登记素材 →
# 导日指标 → 认领对象 → 定格发现 → 提交复盘 → 沉淀经验。
#
# 和 scripts/seed-insight-{performance,assets,drivers}-demo.sh 的区别：那三个脚本
# 假设项目和数据源已经存在，只补数据；这个脚本从建项目开始，一遍跑完整条链路，
# 让「分析 / 复盘 / 经验 / 素材 / 投前」五个入口同时有东西可看。
#
# 全程走真实接口，不直接写库；可重复执行：项目按名字查重，素材按标题查重，
# 日指标是 upsert（uq_insight_metric_daily_fact），重复跑不会翻倍。
#
# 数据是刻意设计的，不是随机噪声：
#   AD-2001 v1  稳定             —— 趋势里的「持平」，也是对比的基准
#   AD-2002 v2  曝光升、点击率降 —— 疲劳最典型的形态
#   AD-2003 v3  利益开场、偏弱   —— 和 v2 同钩子，凑驱动因素的一组
#   AD-2004 v4  问题开场、偏强   —— 和 v1 同钩子，凑驱动因素的另一组
#   AD-2005 公众号长文           —— 中间一天冲高、一天缺数，异常的两种形态
#   AD-2006 达人混剪             —— 故意不认领，演示「对不上号」那一列
#
# 用法：
#   bash scripts/seed-insight-showcase.sh                     # 打本机 8080
#   COOKIES_API=http://14.103.24.58:8091 bash scripts/seed-insight-showcase.sh
set -euo pipefail

# Windows 上 python 默认按 gbk 写 stdout，中文素材名会直接炸在编码上。
export PYTHONUTF8=1 PYTHONIOENCODING=utf-8

BASE="${COOKIES_API:-http://127.0.0.1:8080}"
PROJECT_NAME="${COOKIES_DEMO_PROJECT_NAME:-Nova 家清 · 夏季清洁新品投放}"
USERNAME="${COOKIES_ADMIN_USERNAME:-Admin}"
PASSWORD="${COOKIES_ADMIN_PASSWORD:-123456}"

python - "$BASE" "$PROJECT_NAME" "$USERNAME" "$PASSWORD" <<'PY'
import datetime
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
        request.add_header("Idempotency-Key", f"seed-showcase-{abs(hash(url + str(data))):x}")
    try:
        with opener.open(request) as response:
            raw = response.read()
    except urllib.error.HTTPError as err:
        if err.code in tolerate:
            return None
        raise SystemExit(f"{method} {url} -> {err.code} {err.read().decode('utf-8', 'replace')}")
    return json.loads(raw) if raw else {}


call("POST", f"{BASE}/platform/v1/auth/login", {"username": USERNAME, "password": PASSWORD})

# --- 1. 项目 ---------------------------------------------------------------
project = next((item for item in call("GET", f"{BASE}/platform/v1/projects")["items"]
                if item["name"] == PROJECT_NAME), None)
if project is None:
    brand = call("POST", f"{BASE}/platform/v1/brands", {"name": "Nova 家清"})
    project = call("POST", f"{BASE}/platform/v1/projects", {
        "name": PROJECT_NAME,
        "brand": "Nova 家清",
        "goal": "夏季清洁新品首轮抖音投放，验证「问题开场」和「利益开场」哪种钩子更扛得住。",
        "industry": "ecommerce",
        "primary_brand_id": brand["id"],
        "product_ids": [],
        "activate": True,
    })
    print(f"新建项目 {project['id']} {PROJECT_NAME}")
else:
    print(f"项目已存在，复用 {project['id']} {PROJECT_NAME}")

PROJECT = project["id"]
ROOT = f"{BASE}/api/insights/v1/projects/{PROJECT}"

# --- 2. 数据源 -------------------------------------------------------------
# 字段映射不能空：空映射的数据源会在导入那一步被直接拒掉（「先完成接入向导」）。
sources = call("GET", f"{ROOT}/data-sources")["items"]
if sources:
    source = sources[0]
    print(f"数据源已存在，复用 {source['id']}")
else:
    source = call("POST", f"{ROOT}/data-sources", {
        "platform": "douyin",
        "account_label": "Nova 家清 · 抖音主账户",
        "account_ref": "nova-douyin-main",
        "ingest_mode": "file_import",
        "credential_ref": "vault://douyin/nova-main",
        "field_mapping": {
            "展示数": "impressions",
            "点击数": "clicks",
            "转化数": "conversions",
            "播放数": "video_views",
            "完播数": "video_completions",
            "消耗": "spend_cents",
            "成交金额": "revenue_cents",
        },
    })
    print(f"新建数据源 {source['id']}")
SOURCE_ID = source["id"]

# --- 3. 素材 ---------------------------------------------------------------
# 特征是照着标题写的，不是为了让图好看编的。hook_type 的词条顺序必须一致——
# 驱动因素按取值文本严格相等分组，顺序一变就分成两组，那一格立刻变空。
ASSETS = [
    {
        "title": "Nova 夏季清洁 数字人口播 v1（问题开场）",
        "asset_type": "digital_human_ad", "object_id": "AD-2001",
        "features": [
            {"key": "hook_type", "value": {"kind": "enum_multi", "terms": ["问题", "反差"]}},
            {"key": "presenter", "value": {"kind": "enum", "terms": ["数字人"]}},
            {"key": "selling_points", "value": {"kind": "tags", "terms": ["强效去油", "免手洗"]}},
        ],
    },
    {
        "title": "Nova 夏季清洁 数字人口播 v2（利益开场）",
        "asset_type": "digital_human_ad", "object_id": "AD-2002",
        "features": [
            {"key": "hook_type", "value": {"kind": "enum_multi", "terms": ["利益"]}},
            {"key": "presenter", "value": {"kind": "enum", "terms": ["数字人"]}},
            {"key": "selling_points", "value": {"kind": "tags", "terms": ["强效去油", "免手洗"]}},
        ],
    },
    {
        "title": "Nova 夏季清洁 数字人口播 v3（利益开场·紧字幕）",
        "asset_type": "digital_human_ad", "object_id": "AD-2003",
        "features": [
            {"key": "hook_type", "value": {"kind": "enum_multi", "terms": ["利益"]}},
            {"key": "presenter", "value": {"kind": "enum", "terms": ["数字人"]}},
            {"key": "selling_points", "value": {"kind": "tags", "terms": ["强效去油", "免手洗"]}},
        ],
    },
    {
        "title": "Nova 夏季清洁 数字人口播 v4（问题开场·反差结尾）",
        "asset_type": "digital_human_ad", "object_id": "AD-2004",
        "features": [
            {"key": "hook_type", "value": {"kind": "enum_multi", "terms": ["问题", "反差"]}},
            {"key": "presenter", "value": {"kind": "enum", "terms": ["数字人"]}},
            {"key": "selling_points", "value": {"kind": "tags", "terms": ["强效去油", "免手洗"]}},
        ],
    },
    {
        # 故意不填特征：素材入口的「待提取变量」那一列要有东西，
        # 演示「素材在库里，但还没人说清它长什么样」。
        "title": "Nova 夏季清洁 公众号长文",
        "asset_type": "wechat_article", "object_id": "AD-2005",
        "features": [],
    },
]

existing = {item["title"]: item for item in call("GET", f"{ROOT}/assets?limit=200")["items"]}
for spec in ASSETS:
    asset = existing.get(spec["title"])
    if asset is None:
        asset = call("POST", f"{ROOT}/assets", {
            "title": spec["title"],
            "source_kind": "upload",
            "asset_type": spec["asset_type"],
            "asset_type_source": "human",
        })
        if spec["features"]:
            call("PATCH", f"{ROOT}/assets/{asset['id']}/features", {
                "expected_version": asset["version"],
                "reason": "从交付件原片登记钩子、出镜形式和卖点。",
                "features": [dict(item, review_state="confirmed") for item in spec["features"]],
            })
        print(f"新建素材 {asset['id']} {spec['title']}")
    else:
        print(f"素材已存在，复用 {asset['id']} {spec['title']}")
    spec["asset_id"] = asset["id"]

# --- 4. 日指标 -------------------------------------------------------------
DAYS = 21
end = datetime.date.today()
start = end - datetime.timedelta(days=DAYS - 1)
rows = []


def add(object_id, name, day, impressions, ctr, cvr, cpm_cents=7200, rev_per_conv=24000):
    impressions = max(1, round(impressions))
    clicks = round(impressions * ctr)
    conversions = round(clicks * cvr)
    rows.append({
        "platform_object_kind": "ad",
        "platform_object_id": object_id,
        "platform_object_name": name,
        "stat_date": (start + datetime.timedelta(days=day)).isoformat(),
        "counts": {
            "impressions": impressions,
            "clicks": clicks,
            "conversions": conversions,
            "video_views": round(impressions * 0.62),
            "video_completions": round(impressions * 0.18),
            "spend_cents": round(impressions / 1000 * cpm_cents),
            "revenue_cents": conversions * rev_per_conv,
        },
    })


for day in range(DAYS):
    add("AD-2001", "夏季清洁-数字人口播-v1", day,
        9200 + (day % 3) * 400, 0.042 + (day % 4 - 1.5) * 0.0009, 0.066, 7800)
    add("AD-2002", "夏季清洁-数字人口播-v2", day,
        8000 + day * 500, 0.033 - day * 0.00075, 0.052, 7000)
    add("AD-2003", "夏季清洁-数字人口播-v3", day,
        7600 + (day % 4) * 260, 0.024, 0.049, 7100)
    add("AD-2004", "夏季清洁-数字人口播-v4", day,
        8100 + (day % 4) * 260, 0.044, 0.063, 7300)
    # 公众号长文：第 12 天被大号转了一次，第 16 天完全没有回流。
    # 基线要带正常抖动——零波动的序列会被异常检测直接跳过，那一天的冲高会凭空消失。
    if day != 16:
        add("AD-2005", "夏季清洁-公众号长文", day,
            (4000 + (day % 5) * 210) * (4.5 if day == 12 else 1.0), 0.014, 0.02, 7300)
    # 达人混剪：跑了但没人认领，留在「待匹配」。
    add("AD-2006", "夏季清洁-达人混剪-投放测试", day,
        3000 + (day % 3) * 180, 0.02, 0.03, 6900)

batch = call("POST", f"{ROOT}/import-batches", {
    "data_source_id": SOURCE_ID,
    "kind": "backfill",
    "source_label": "演示数据 · 夏季清洁首轮投放",
    "rows": rows,
    "register_objects": True,
})
batch = batch.get("batch", batch)
print(f"导入 {start} ~ {end}：接受 {batch.get('accepted_rows')} 拒绝 {batch.get('rejected_rows')}")

# --- 5. 认领 ---------------------------------------------------------------
targets = {spec["object_id"]: spec["asset_id"] for spec in ASSETS}  # AD-2006 不在里面
for mapping in call("GET", f"{ROOT}/asset-mappings?limit=200")["items"]:
    asset_id = targets.get(mapping["platform_object_id"])
    if asset_id is None or mapping.get("asset_id") == asset_id:
        continue
    call("POST", f"{ROOT}/asset-mappings/{mapping['id']}:resolve", {
        "expected_version": mapping["version"],
        "status": "matched",
        "asset_id": asset_id,
        "note": "投放计划书里这条创意用的就是这条素材的原片。",
    })
    print(f"认领 {mapping['platform_object_id']} → {asset_id}")

# --- 6. 定格发现 -----------------------------------------------------------
# 定格的主语必须能在当次分析结果里找回来（findJudgement），所以这里不写死，
# 而是读回真实的分析结果再挑。挑不到就跳过，宁可少一条，也不能造一条假的。
window = {"start": start.isoformat(), "end": end.isoformat()}

# 重跑时这个窗口上多半已经有一份提交过的复盘了。再定格一次会另起一份草稿，
# 提交时撞上 (执行, 窗口) 的唯一键——所以先认已有的那一份。
report = next((item for item in call("GET", f"{ROOT}/reports?limit=50")["items"]
               if item.get("window_start") == window["start"]
               and item.get("window_end") == window["end"]
               and item.get("status") == "confirmed"), None)
if report is not None:
    print(f"这个窗口已有复盘 {report['id']}，跳过定格和提交")

pins = []
if report is None:
    analysis = call(
        "GET", f"{ROOT}/performance-analysis?start={window['start']}&end={window['end']}")
    for item in (analysis.get("fatigue") or [])[:1]:
        pins.append({"dimension": "fatigue", "source_ref": item["asset_id"],
                     "text": "v2 曝光越加越多、点击率一路下滑，这一轮先把它的预算收回来。"})
    for item in (analysis.get("drivers") or [])[:1]:
        pins.append({"dimension": "drivers", "variable": item["key"],
                     "text": "钩子类型这一格上两组差得开，下一轮的脚本先按这个走。"})
    for item in (analysis.get("anomalies") or [])[:1]:
        pins.append({"dimension": "anomalies", "source_ref": item["asset_id"],
                     "text": f"{item.get('date')} 这一天的数不对劲，回头找平台核一下。"})

for pin in pins:
    body = {"window": window, "dimension": pin["dimension"], "text": pin["text"]}
    if "source_ref" in pin:
        body["source_ref"] = pin["source_ref"]
    if "variable" in pin:
        body["variable"] = pin["variable"]
    result = call("POST", f"{ROOT}/findings", body, tolerate=(400, 409))
    if result is None:
        print(f"定格 {pin['dimension']} 没对上号，跳过")
        continue
    report = result.get("report", result)
    print(f"定格一条：{pin['dimension']}")

if report is None:
    print("没有可定格的发现，复盘和经验这两步跳过。")
    print(f"\n项目 ID：{PROJECT}")
    raise SystemExit(0)

# --- 7. 提交复盘 -----------------------------------------------------------
# 提交会把系统发现一起定格进 digest，报告随之变成「已确认」。
if report["status"] != "confirmed":
    report = call("POST", f"{ROOT}/reports/{report['id']}/submit", {
        "execution_id": f"exec_nova_summer_{start.isoformat()}",
        "expected_version": report["version"],
    })
print(f"复盘 {report['id']}：共 {len(report.get('digest') or [])} 条发现")

# --- 8. 沉淀经验 -----------------------------------------------------------
# 适用范围要填满：投前是按「这一轮的品牌/渠道/广告类型」回查经验的，
# 适用范围空着的经验查不出来，经验库里看着有、投前却一条都不出。
# 类型只有事实 / 统计观察 / 假设 / 建议四种，置信只有充分 / 方向性 / 样本不足 / 存在混杂。
EXPERIENCES = [
    {
        "conclusion": "夏季清洁品类的数字人口播，问题开场比利益开场更扛跑：同预算下点击率高出约 1.8 个百分点，且三周内没有明显衰减。",
        "conditions": ["抖音信息流", "数字人口播", "夏季清洁品类", "单条曝光 ≥ 1 万"],
        "counterexamples": ["公众号长文不适用：长文的开场承担的是留存而不是点击。"],
        "card_type": "statistic",
        "confidence": "sufficient",
        "recommended_action": "下一轮脚本先出两版问题开场，利益开场只留一版做对照。",
        "applicability": {
            "brands": ["Nova 家清"], "channels": ["抖音"],
            "creative_types": ["数字人口播"], "objectives": ["新品拉新"],
            "time_range_note": "夏季清洁旺季（6—8 月）",
        },
        "content_basis": {"features": ["hook_type"], "note": "钩子类型按 2 对 2 分组比出来的。"},
        # 「事实」和「统计观察」两档必须给数据依据，否则后端会退回：没有依据的
        # 统计观察就是假设，只是换了个更唬人的名字。
        "data_basis": {
            "asset_count": 4,
            "sample_size": sum(row["counts"]["impressions"] for row in rows
                               if row["platform_object_id"] in
                               ("AD-2001", "AD-2002", "AD-2003", "AD-2004")),
            "metrics": ["ctr", "cvr"],
            "baseline": "利益开场组（v2 / v3）",
        },
        "confirm": True,
    },
    {
        "conclusion": "同一版素材连续投放到第三周，曝光还在加、点击率却持续走低时，加预算换不回量，应该换素材而不是加钱。",
        "conditions": ["抖音信息流", "同一素材连续投放 ≥ 14 天"],
        "counterexamples": [],
        "card_type": "hypothesis",
        "confidence": "directional",
        "recommended_action": "监控里看到这个形态，先停投再排期新素材。",
        "applicability": {
            "brands": ["Nova 家清"], "channels": ["抖音"], "creative_types": ["数字人口播"],
        },
        "content_basis": {"note": "只看了一条素材的疲劳形态，还需要下一轮验证。"},
        "data_basis": {"asset_count": 1, "metrics": ["impressions", "ctr"]},
        "confirm": False,
    },
]

deposited = {item["conclusion"] for item in call("GET", f"{ROOT}/experiences?limit=200")["items"]}
for spec in EXPERIENCES:
    if spec["conclusion"] in deposited:
        print("经验已存在，跳过")
        continue
    created = call("POST", f"{ROOT}/reports/{report['id']}:create-experience", {
        "expected_report_version": report["version"],
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
    print(f"沉淀经验 {created['id']}")
    if spec["confirm"]:
        call("POST", f"{ROOT}/experiences/{created['id']}:confirm",
             {"expected_version": created["version"]})
        print("  已确认，投前那一栏能查到它")

print(f"\n完成。项目 ID：{PROJECT}")
print("在页面上切到这个项目，五个入口都有东西：")
print("  分析 —— 六个视图，趋势/疲劳/异常/驱动因素都出得来结论")
print("  素材 —— 5 条素材，公众号长文停在「待提取变量」")
print("  复盘 —— 一份已确认的复盘，人记的在前、系统补的在后")
print("  经验 —— 两条，一条在用、一条待确认")
print("  接入 —— 一个数据源、一批导入、AD-2006 停在「待匹配」")
PY
