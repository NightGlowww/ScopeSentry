// UI 冒烟：资产页 13 个 Tab 的「?tab=&search= 过滤 + 空状态」矩阵（2026-11-20 全量实测通过）。
// 用法：逐个 Tab 打开 #/asset-information/index?tab=<name>&search=task=="no-such-task"，
// 然后 eval 本文件内容；返回 {ok, checks:[{tab, pass, detail}]}。
// 注意：单次 eval 只检查"当前已渲染的那个 Tab"，所以调用方需要逐个 Tab 切换后再 eval。
(async () => {
  const vis = e => e && e.offsetParent !== null;
  const rows = [...document.querySelectorAll(".el-table__body-wrapper tbody tr")].filter(vis).length;
  const empties = [...document.querySelectorAll(".el-table__empty-text")].filter(vis).map(e => e.innerText.replace(/\s+/g, " ").trim());
  const searchVal = [...document.querySelectorAll("input")].filter(vis).map(i => i.value).find(v => v && v.includes("task=="));
  const active = [...document.querySelectorAll(".el-tabs__item")].filter(vis).find(t => t.className.includes("is-active"));
  const isContextual = (empties[0] || "").includes("当前筛选");
  const checks = [
    { name: "搜索条件已带入", pass: !!searchVal, detail: searchVal || "empty" },
    { name: "列表被过滤为 0 行", pass: rows === 0, detail: rows + " 行" },
    { name: "空状态说明原因", pass: isContextual, detail: empties[0] || "none" }
  ];
  return JSON.stringify({ tab: active ? active.innerText.trim() : null, ok: checks.every(c => c.pass), checks });
})()