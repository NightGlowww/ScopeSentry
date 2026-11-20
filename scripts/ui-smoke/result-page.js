// UI 冒烟：漏洞结果页（在 #/asset-information/nuclei-result 上执行）
(async () => {
  const vis = e => e && e.offsetParent !== null;
  const sleep = ms => new Promise(r => setTimeout(r, ms));
  const checks = [];
  const add = (name, pass, detail) => checks.push({ name, pass: !!pass, detail: String(detail ?? "") });

  const bar = [...document.querySelectorAll(".triage-bar")].filter(vis)[0];
  add("处置进度条存在", !!bar, bar ? bar.innerText.replace(/\s+/g, " ").trim() : "missing");

  const taskInput = [...document.querySelectorAll("input")].filter(vis).find(i => (i.placeholder || "").includes("任务"));
  add("按任务筛选输入框存在", !!taskInput, taskInput ? taskInput.placeholder : "missing");

  const rows = [...document.querySelectorAll(".el-table__body-wrapper tbody tr")].filter(vis);
  add("结果列表有数据", rows.length > 0, rows.length + " 行");

  if (rows.length) {
    const cell = [...rows[0].querySelectorAll("td")].filter(vis)[2] || rows[0];
    cell.click();
    await sleep(2200);
    const drawer = [...document.querySelectorAll(".el-drawer")].filter(vis)[0];
    const hasPoc = drawer ? drawer.innerText.includes("POC 模板") : false;
    add("行点击打开详情且含 POC 模板", !!drawer && hasPoc, drawer ? drawer.innerText.replace(/\s+/g, " ").slice(0, 40) : "no drawer");
    const close = drawer ? [...drawer.querySelectorAll(".el-drawer__close-btn, button")].find(b => b.getAttribute("aria-label") === "Close") : null;
    close && close.click();
    await sleep(500);
  }

  const btnTexts = [...document.querySelectorAll(".batch-bar button")].filter(vis).map(b => b.innerText.trim());
  add("批量处置按钮存在", btnTexts.some(t => t.includes("批量")), btnTexts.join("|") || "none");

  return JSON.stringify({ ok: checks.every(c => c.pass), checks });
})()