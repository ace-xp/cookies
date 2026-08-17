import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";

/**
 * 名词表的前端那一半。
 *
 * 设置 → 变量字典 → 名词表整屏的立论是「表上『不要再叫』的说法不许再出现在任何
 * 用户能看见的文案里，有测试拦着」。但拦着的那个测试（internal/systems/insights/
 * glossary_test.go）只查后端那十二个枚举的中文名，前端一个文件都没看——而人看见
 * 废词的地方几乎全在前端。这里补上后半截。
 *
 * 废词表不在这个文件里重抄一遍，是从 glossary.go 里读出来的：抄一份的后果是
 * 名词表改了、这个测试还按老表拦，页面上说的和拦的又不是一回事。
 */
const repoRoot = join(import.meta.dirname, "..");

function bannedAliases(): string[] {
  const source = readFileSync(join(repoRoot, "internal/systems/insights/glossary.go"), "utf8");
  const aliases: string[] = [];
  for (const match of source.matchAll(/Avoid:\s*\[\]string\{([^}]*)\}/g)) {
    for (const quoted of match[1].matchAll(/"([^"]+)"/g)) aliases.push(quoted[1]);
  }
  return aliases;
}

/**
 * 素材洞察这个模块用户看得见的文案在哪些文件里。
 *
 * insight/ 目录是六个入口的本体；下面四个页面是被入口内嵌进去的（素材 · 数据接入
 * 渲染 DataConnectionsPage，素材 · 变量渲染 ContentAnalysisPage，设置的两屏同理），
 * 人分不出它们是另一个文件，废词出现在那里和出现在入口里没有区别。
 */
const scanned = [
  "src/components/insight",
  "src/components/CapabilityOperationsPage.tsx",
  "src/components/DataQualityPage.tsx",
  "src/components/DataConnectionsPage.tsx",
  "src/components/ContentAnalysisPage.tsx",
  "src/components/ExperienceReviseForm.tsx",
  "src/data/verdict.ts",
  "src/data/insightCard.ts",
];

function collect(relative: string): string[] {
  const absolute = join(repoRoot, relative);
  if (!statSync(absolute).isDirectory()) return [relative];
  return readdirSync(absolute).flatMap(entry => collect(join(relative, entry)));
}

/**
 * 去掉注释再查。
 *
 * 注释是写给改代码的人看的，那里说「原来叫『沉淀』，改了」是有用的交代，拦掉反而
 * 让人没法记录一个词为什么被废。用字符扫描而不是正则：字符串里出现的 `//`（比如
 * 一个 URL）不是注释，正则分不出来。
 */
function stripComments(source: string): string {
  let out = "";
  let index = 0;
  let quote = "";
  while (index < source.length) {
    const char = source[index];
    const next = source[index + 1];
    if (quote) {
      if (char === "\\") { out += char + (next ?? ""); index += 2; continue; }
      if (char === quote) quote = "";
      out += char;
      index += 1;
      continue;
    }
    if (char === '"' || char === "'" || char === "`") { quote = char; out += char; index += 1; continue; }
    if (char === "/" && next === "/") {
      while (index < source.length && source[index] !== "\n") index += 1;
      continue;
    }
    if (char === "/" && next === "*") {
      index += 2;
      while (index < source.length && !(source[index] === "*" && source[index + 1] === "/")) index += 1;
      index += 2;
      continue;
    }
    out += char;
    index += 1;
  }
  return out;
}

test("素材洞察的前端文案里没有名词表废掉的说法", () => {
  const aliases = bannedAliases();
  assert.ok(aliases.length >= 20, "废词表读空了，说明 glossary.go 的写法变了，解析要跟着改");

  const offences: string[] = [];
  for (const relative of scanned.flatMap(collect)) {
    if (!/\.(ts|tsx)$/.test(relative)) continue;
    const text = stripComments(readFileSync(join(repoRoot, relative), "utf8"));
    text.split("\n").forEach((line, index) => {
      for (const alias of aliases) {
        if (line.includes(alias)) offences.push(`${relative}:${index + 1} 用了「${alias}」：${line.trim()}`);
      }
    });
  }

  assert.deepEqual(offences, [], `\n${offences.join("\n")}\n`);
});

/**
 * 侧栏的入口名、介绍语和视图名和页面正文同等重要——「已沉淀经验」当年就是个视图名。
 * navigation.ts 里六个系统的导航写在一个文件里，只截素材洞察那一段：别的系统不归
 * 这张名词表管，整文件扫会把策略中心的文案也拦下来。
 */
test("素材洞察的侧栏入口名与视图名里没有废词", () => {
  const source = readFileSync(join(repoRoot, "src/data/navigation.ts"), "utf8");
  const start = source.indexOf("key: 'insight'");
  assert.ok(start > 0, "navigation.ts 里找不到素材洞察那一段，截取方式要跟着改");
  const end = source.indexOf("key: '", source.indexOf("nav: [", start));
  const block = stripComments(source.slice(start, end > start ? end : undefined));

  const offences = bannedAliases().filter(alias => block.includes(alias));
  assert.deepEqual(offences, [], `素材洞察的导航文案里出现了废词：${offences.join("、")}`);
});

test("名词表里每个批准的词都有解释，没有空条目", () => {
  const source = readFileSync(join(repoRoot, "internal/systems/insights/glossary.go"), "utf8");
  const terms = [...source.matchAll(/Term:\s*"([^"]+)",\s*\n\s*Means:\s*"([^"]+)"/g)];
  assert.ok(terms.length >= 12, "名词表条目少于 12 条，解析或表本身出了问题");
  for (const [, term, means] of terms) {
    assert.ok(means.length > 8, `「${term}」的解释太短，等于没解释`);
  }
});
