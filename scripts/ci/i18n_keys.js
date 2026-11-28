#!/usr/bin/env node
// i18n key 闭环检查：确保「页面引用的 key」在两个语言里都真实存在。
//
// 为什么需要：i18n_guard.py 只管「.vue 里有没有硬编码中文」，管不了反方向 ——
// t('x.y') 写错一个字母、或者只在一个语言里加了 key，运行期就会把原始 key 直接显示给用户，
// 而这种问题单测、类型检查、打包全都看不出来。
//
// 做法：直接把 locales/*.ts 当数据求值（去掉 export default），flatten 成点路径，
// 再扫所有 .vue/.ts/.tsx 里的 t('...') / $t('...') / i18n.global.t('...')。
//
// 判定：
//   真实缺失（会渲染成 key）→ FAIL
//   两个语言 key 集合不一致      → FAIL（一端有另一端没有，切语言就露馅）
//   views/Components/ 下的 demo 页 → 只警告（这些页面没挂路由）
//   以 . 结尾的前缀（动态拼接）    → 只警告
const fs = require('fs');
const path = require('path');
const vm = require('vm');

const ROOT = path.resolve(__dirname, '../../frontend/src');
const LOCALES = path.join(ROOT, 'locales');

function loadLocale(file) {
  const src = fs.readFileSync(file, 'utf8').replace(/^export default/m, 'module.exports =');
  const sandbox = { module: { exports: {} } };
  sandbox.exports = sandbox.module.exports;
  vm.runInNewContext(src, sandbox, { filename: file });
  return sandbox.module.exports;
}

function flat(o, prefix, out) {
  for (const k of Object.keys(o)) {
    const v = o[k];
    const key = prefix ? prefix + '.' + k : k;
    if (v && typeof v === 'object' && !Array.isArray(v)) flat(v, key, out);
    else out[key] = v;
  }
  return out;
}

const zh = flat(loadLocale(path.join(LOCALES, 'zh-CN.ts')), '', {});
const en = flat(loadLocale(path.join(LOCALES, 'en.ts')), '', {});

const files = [];
(function walk(d) {
  for (const e of fs.readdirSync(d, { withFileTypes: true })) {
    const p = path.join(d, e.name);
    if (e.isDirectory()) {
      if (e.name === 'locales' || e.name === 'node_modules') continue;
      walk(p);
    } else if (/\.(vue|ts|tsx)$/.test(e.name)) files.push(p);
  }
})(ROOT);

const re = /(?:\bt|\$t|i18n\.global\.t)\(\s*['"]([A-Za-z0-9_.\-]+)['"]/g;
const used = new Map();
for (const f of files) {
  const src = fs.readFileSync(f, 'utf8');
  let m;
  while ((m = re.exec(src))) {
    const k = m[1];
    if (!k.includes('.')) continue;
    const rel = f.slice(ROOT.length + 1);
    if (!used.has(k)) used.set(k, []);
    if (!used.get(k).includes(rel)) used.get(k).push(rel);
  }
}

let fail = 0;
const demoMiss = [], dynMiss = [], realMiss = [];
for (const [k, srcs] of used) {
  if (k in zh && k in en) continue;
  if (k.endsWith('.')) { dynMiss.push([k, srcs[0]]); continue; }
  if (srcs.every((s) => s.startsWith('views/Components/'))) { demoMiss.push([k, srcs[0]]); continue; }
  realMiss.push([k, srcs[0]]);
}

const zhOnly = Object.keys(zh).filter((k) => !(k in en));
const enOnly = Object.keys(en).filter((k) => !(k in zh));

console.log('扫描 %d 个文件，引用 %d 个 key；zh %d / en %d', files.length, used.size, Object.keys(zh).length, Object.keys(en).length);
if (zhOnly.length || enOnly.length) {
  fail = 1;
  console.log('语言 key 集合不一致：zh-only %d，en-only %d', zhOnly.length, enOnly.length);
  zhOnly.slice(0, 10).forEach((k) => console.log('  zh-only ' + k));
  enOnly.slice(0, 10).forEach((k) => console.log('  en-only ' + k));
}
if (realMiss.length) {
  fail = 1;
  console.log('真实缺失 %d 个（运行期会显示原始 key）：', realMiss.length);
  realMiss.slice(0, 20).forEach(([k, s]) => console.log('  MISS ' + k + '  <- ' + s));
}
if (demoMiss.length) console.log('demo 页缺失 %d 个（views/Components/ 未挂路由，仅提示）', demoMiss.length);
if (dynMiss.length) console.log('动态前缀 %d 个（以 . 结尾，拼接用）: %s', dynMiss.length, dynMiss.map((d) => d[0]).join(', '));
console.log('I18N_KEYS=' + (fail ? 'FAIL' : 'PASS'));
process.exit(fail);