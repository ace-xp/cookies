import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

test('米云素材保持为素材洞察下的独立路由页面，冻结四个一级视图', async () => {
  const [navigation, pages] = await Promise.all([
    readFile(new URL('../src/data/navigation.ts', import.meta.url), 'utf8'),
    readFile(new URL('../src/components/Pages.tsx', import.meta.url), 'utf8'),
  ])

  // group 不写死：上游挂在 '素材与分析'，那个分组在我们这边的导航重构里已经合掉了，
  // 米云现在挂 '工作'。这条测试要冻的是「独立路由页 + 四个一级视图」，不是它归哪一组。
  assert.match(navigation, /id: 'miyun-materials', label: '米云素材', icon: Aperture, group: '[^']+', layout: 'workspace'/)
  assert.match(navigation, /views: \['产品分析', '采集任务', '素材候选', '裂变任务'\]/)
  // 只冻到 activeView 为止，后面的 prop 不锁死：这条测试要冻的是「米云是独立路由页」，
  // 不是它接几个参数。往下加 prop（比如把人送去分析那条路）不该让这条测试失败。
  assert.match(pages, /system\.key === 'insight' && item\.id === 'miyun-materials' \? <MiyunMaterialsPage state=\{dataState\} activeView=\{activeView\}/)
})

test('米云已导入的素材在这一页上能走到内容分析去', async () => {
  const [pages, miyun] = await Promise.all([
    readFile(new URL('../src/components/Pages.tsx', import.meta.url), 'utf8'),
    readFile(new URL('../src/components/MiyunMaterialsPage.tsx', import.meta.url), 'utf8'),
  ])

  // 米云导进来的素材是分析对象（Role: analysis），判形态、提变量都在「素材 → 变量」
  // 那一屏。这一页原来一个字都没提它在哪，人看着一条「已导入」没有下一步可走。
  assert.match(pages, /onOpenAssetAnalysis=\{assetId => onOpenProject\(currentProject\.id, 'insight', 'assets', assetId, '变量'\)\}/)
  // 只有真导进来、真拿到了洞察素材 ID 的才给这个入口——候选和导入失败的点过去会落空。
  assert.match(miyun, /material\.import_status === "imported" && material\.insight_asset_id/)
})
