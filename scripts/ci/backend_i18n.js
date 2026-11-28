#!/usr/bin/env node
// 后端错误/成功文案的 i18n 闭环：Go 里 response.Xxx(c, "api.xxx") 用的 key，
// 必须在三个语言里都有词条，否则用户会在界面上看到 "api.task.target_null" 这种裸 key。
//
// 为什么单独查：i18n_keys.js 只扫前端的 t('...')，管不到后端返回的 message。
// 前端 translateMessage 只在 te(key) 命中时才翻译，未知 key 原样透出（.success 兜底除外）。
const fs = require('fs');
const path = require('path');
const vm = require('vm');

const ROOT = path.resolve(__dirname, '../..');
const LOCALES = path.join(ROOT, 'frontend/src/locales');
const GO_ROOT = path.join(ROOT, 'internal');

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

function walkGo(dir, out) {
  for (const name of fs.readdirSync(dir)) {
    const p = path.join(dir, name);
    const st = fs.statSync(p);
    if (st.isDirectory()) walkGo(p, out);
    else if (name.endsWith('.go') && !name.endsWith('_test.go')) out.push(p);
  }
  return out;
}

// 只认 response.* 帮助函数里的字符串字面量，避免把普通字符串误判成 key
const CALL_RE = /response\.(?:Success|BadRequest|BadRequestWithError|InternalServerError|NotFound|Unauthorized|Forbidden|BadRequestWithError)\(([\s\S]{0,400}?)\)/g;
const KEY_RE = /"([a-z][A-Za-z0-9_]*(?:\.[A-Za-z0-9_]+)+)"/g;

const keys = new Map(); // key -> 首个出处
for (const file of walkGo(GO_ROOT, [])) {
  const src = fs.readFileSync(file, 'utf8');
  let m;
  while ((m = CALL_RE.exec(src))) {
    let k;
    KEY_RE.lastIndex = 0;
    while ((k = KEY_RE.exec(m[1]))) {
      const key = k[1];
      // 形如 api.xxx / common.xxx 这类带点的 key；排除文件路径与 URL
      if (!keys.has(key)) keys.set(key, path.relative(ROOT, file));
    }
  }
}

const locales = {
  'zh-CN': flat(loadLocale(path.join(LOCALES, 'zh-CN.ts')), '', {}),
  en: flat(loadLocale(path.join(LOCALES, 'en.ts')), '', {}),
  'ko-KR': flat(loadLocale(path.join(LOCALES, 'ko-KR.ts')), '', {})
};

// 与 frontend/src/axios/translateMessage.ts 的 SUFFIX_FALLBACK 一一对应：
// 这些后缀由前端归类到通用文案，不需要逐条词条。改这里必须同步改那里。
const SUFFIX_FALLBACK = [
  'success', 'failed', 'error', 'not_found', 'unauthorized', 'forbidden', 'bad_request',
  'required', 'required_fields', 'invalid', 'invalid_params', 'invalid_type', 'invalid_action',
  'invalid_fields', 'invalid_filename', 'invalid_file_type', 'id_missing', 'blank', 'negative',
  'no_files_selected', 'invalidParams', 'invalid_flag', 'name_exists'
];

// 族级前缀：api.required.* / api.bad_request.* 共用一条通用文案
const PREFIX_FALLBACK = ['api.required.', 'api.bad_request.'];

const covered = (key) =>
  PREFIX_FALLBACK.some((p) => key.startsWith(p)) ||
  SUFFIX_FALLBACK.some((s) => key.endsWith('.' + s) || key.endsWith('_' + s));

let missing = 0;
let fallback = 0;
const report = [];
for (const [key, src] of [...keys].sort()) {
  const lack = Object.keys(locales).filter((l) => !(key in locales[l]));
  if (!lack.length) continue;
  if (covered(key)) {
    fallback += 1;
    continue;
  }
  missing += 1;
  report.push('    ! ' + key + '  缺：' + lack.join(',') + '  （' + src + '）');
}
console.log(
  '后端文案 key ' + keys.size + ' 个：有词条 ' + (keys.size - missing - fallback) +
  ' / 后缀兜底 ' + fallback + ' / 未覆盖 ' + missing
);
report.slice(0, 40).forEach((l) => console.log(l));
if (missing) {
  console.log('BACKEND_I18N=FAIL（这些 key 没有词条也没有后缀兜底，用户会看到裸 key）');
  process.exit(1);
}
console.log('BACKEND_I18N=PASS');
