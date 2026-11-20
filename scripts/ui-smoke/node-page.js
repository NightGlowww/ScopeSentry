// UI 冒烟：节点页（在 #/node-management/index 上执行）
(async () => {
  const vis = e => e && e.offsetParent !== null;
  const sleep = ms => new Promise(r => setTimeout(r, ms));
  const checks = [];
  const add = (name, pass, detail) => checks.push({ name, pass: !!pass, detail: String(detail ?? "") });

  const headers = [...document.querySelectorAll(".el-table__header th")].filter(vis).map(t => t.innerText.trim()).filter(Boolean);
  add("包含「工具失败」列", headers.includes("工具失败"), headers.join("|"));
  add("包含「队列」列", headers.includes("队列"), "ok");

  const rows = [...document.querySelectorAll(".el-table__body-wrapper tbody tr")].filter(vis);
  add("节点列表非空", rows.length > 0, rows.length + " 个节点");

  if (rows.length) {
    const logBtn = [...rows[0].querySelectorAll("button")].filter(vis).find(b => b.innerText.replace(/\s+/g, "") === "日志");
    add("日志按钮存在", !!logBtn, "ok");
    if (logBtn) {
      logBtn.click();
      await sleep(2600);
      const dlg = [...document.querySelectorAll(".el-dialog")].filter(d => d.getBoundingClientRect().height > 80)[0];
      add("日志弹窗打开", !!dlg, "ok");
      const summary = dlg ? dlg.querySelector(".log-summary__head")?.innerText.replace(/\s+/g, " ").trim() : null;
      add("错误摘要块存在", !!summary, summary || "missing");
      const counts = dlg ? dlg.querySelector(".log-count")?.innerText : null;
      add("日志行数计数存在", !!counts, counts || "missing");
      const closeBtn = dlg ? [...dlg.querySelectorAll("button")].find(b => b.getAttribute("aria-label") === "Close" || b.innerText.trim() === "关闭") : null;
      closeBtn && closeBtn.click();
      await sleep(500);
    }
  }

  return JSON.stringify({ ok: checks.every(c => c.pass), checks });
})()