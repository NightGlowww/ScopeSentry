// UI 冒烟：任务 ↔ 漏洞结果 双向闭环（在任务页 #/task-management/ScanTask 上执行）
// 断言：任务进度弹窗的产出栏 → 点漏洞数跳转（带 taskName）→ 结果页出现「筛选自任务」→ 点返回回到任务页并带 search。
(async () => {
  const vis = e => e && e.offsetParent !== null;
  const sleep = ms => new Promise(r => setTimeout(r, ms));
  const checks = [];
  const add = (name, pass, detail) => checks.push({ name, pass: !!pass, detail: String(detail ?? "") });

  const rows = [...document.querySelectorAll(".el-table__body-wrapper tbody tr")].filter(vis);
  const target = rows.find(r => r.innerText.includes("smoke-")) || rows[0];
  add("任务列表有可测行", !!target, target ? target.innerText.replace(/\s+/g, " ").slice(0, 30) : "none");
  if (!target) return JSON.stringify({ ok: false, checks });

  [...target.querySelectorAll("button")].filter(vis).find(b => b.innerText.replace(/\s+/g, "") === "任务进度")?.click();
  await sleep(3200);
  const bar = [...document.querySelectorAll(".outcome-bar")].filter(vis)[0];
  add("产出栏出现", !!bar, bar ? bar.innerText.replace(/\s+/g, " ").trim() : "missing");

  const vulnTag = bar ? [...bar.querySelectorAll(".el-tag")].find(t => t.innerText.includes("漏洞")) : null;
  add("漏洞标签可点击", vulnTag && getComputedStyle(vulnTag).cursor === "pointer", vulnTag ? vulnTag.innerText.trim() : "missing");
  if (!vulnTag) return JSON.stringify({ ok: false, checks });

  vulnTag.click();
  await sleep(3500);
  const hash1 = location.hash;
  add("跳转到结果页并带 taskName", hash1.includes("nuclei-result") && hash1.includes("taskName="), hash1);

  const linkBar = [...document.querySelectorAll(".task-link-bar")].filter(vis)[0];
  add("结果页显示筛选来源", !!linkBar, linkBar ? linkBar.innerText.replace(/\s+/g, " ").trim().slice(0, 40) : "missing");

  const back = linkBar ? [...linkBar.querySelectorAll("button")].find(b => b.innerText.includes("返回")) : null;
  add("返回按钮存在", !!back, back ? "ok" : "missing");
  if (back) {
    back.click();
    await sleep(3500);
    const hash2 = location.hash;
    add("回到任务页并带 search", hash2.includes("ScanTask") && hash2.includes("search="), hash2);
    const searchVal = [...document.querySelectorAll("input")].filter(vis).map(i => i.value).find(v => v && v.startsWith("smoke-") || (v || "").includes("-"));
    add("搜索框已预填任务名", !!searchVal, searchVal || "empty");
  }

  return JSON.stringify({ ok: checks.every(c => c.pass), checks });
})()