// UI 冒烟：任务页（在 #/task-management/ScanTask 上执行）
// 返回 {ok, checks:[{name, pass, detail}]}，由调用方（agent）判断。
(async () => {
  const vis = e => e && e.offsetParent !== null;
  const sleep = ms => new Promise(r => setTimeout(r, ms));
  const checks = [];
  const add = (name, pass, detail) => checks.push({ name, pass: !!pass, detail: String(detail ?? "") });

  const toolbar = [...document.querySelectorAll(".task-toolbar button")].filter(vis).map(b => b.innerText.trim());
  add("toolbar 按钮齐全", ["新建任务", "按筛选删除", "删除任务"].every(x => toolbar.some(t => t.includes(x))), toolbar.join("|"));

  const statusSel = [...document.querySelectorAll(".locale-search-row .el-select")].filter(vis)[0];
  add("状态筛选存在", !!statusSel, statusSel ? "ok" : "missing");

  const rows0 = [...document.querySelectorAll(".el-table__body-wrapper tbody tr")].filter(vis).length;
  add("列表有数据", rows0 > 0, rows0 + " 行");

  const createBtn = [...document.querySelectorAll(".task-toolbar button")].filter(vis).find(b => b.innerText.includes("新建任务"));
  createBtn && createBtn.click();
  await sleep(1500);
  const dlg = [...document.querySelectorAll(".el-dialog")].filter(d => d.getBoundingClientRect().height > 80)[0];
  add("新建任务弹窗可打开", !!dlg, dlg ? (dlg.querySelector(".el-dialog__title")?.innerText || "opened") : "not opened");
  if (dlg) {
    const cancel = [...dlg.querySelectorAll("button")].find(b => /取消|Cancel/.test(b.innerText));
    cancel && cancel.click();
    await sleep(600);
  }

  return JSON.stringify({ ok: checks.every(c => c.pass), checks });
})()