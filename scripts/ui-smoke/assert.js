// 通用断言库：由 cases.json 的 assert 字段选择执行哪一条。
// 调用方式（agent）：打开 cases.json 里的 hash 后 eval 本文件内容，
// 再从返回的 JSON 里看 ok。文件末尾会根据 window.__ASSERT__ 自动选择（默认 tableOrEmpty）。
(async () => {
  const vis = e => e && e.offsetParent !== null;
  const text = (document.body.innerText || "").replace(/\s+/g, " ");
  const rows = () => [...document.querySelectorAll(".el-table__body-wrapper tbody tr")].filter(vis).length;
  const empties = () => [...document.querySelectorAll(".el-table__empty-text")].filter(vis).map(e => e.innerText.replace(/\s+/g, " ").trim());
  const buttons = () => [...document.querySelectorAll("button")].filter(vis).map(b => b.innerText.trim());
  const headers = () => [...document.querySelectorAll(".el-table__header th")].filter(vis).map(t => t.innerText.trim()).filter(Boolean);

  const checks = [];
  const add = (n, p, d) => checks.push({ name: n, pass: !!p, detail: String(d ?? "") });

  const mode = window.__ASSERT__ || "tableOrEmpty";
  if (mode === "tableOrEmpty") {
    const r = rows();
    const e = empties()[0] || null;
    add("有数据或明确空状态", r > 0 || !!e, r + " 行 / " + (e || "无空状态"));
  } else if (mode === "contextualEmpty") {
    const r = rows();
    const e = empties()[0] || "";
    add("列表为空", r === 0, r + " 行");
    // 两套文案都要接受：资产页「当前筛选（任务：x）没有结果」，
    // 漏洞结果页「该任务（x）未扫出漏洞 / 该主机（x）暂无漏洞命中」
    const contextual = /当前筛选|该任务|该主机|未扫出|暂无漏洞|no results|No vulnerabilities/i.test(e)
    add("空状态说明原因", contextual, e || "none");
  } else if (mode === "hasToolbar") {
    const b = buttons();
    add("工具栏按钮存在", b.some(x => x.includes("新建任务")) && b.some(x => x.includes("按筛选删除")), b.slice(0, 5).join("|"));
    add("列表有数据", rows() > 0, rows() + " 行");
  } else if (mode === "hasColumns") {
    const h = headers();
    add("包含工具失败列", h.includes("工具失败"), h.join("|"));
    add("节点非空", rows() > 0, rows() + " 节点");
  } else if (mode === "dialogOpens") {
    // 点「新建/新建 X」按钮，断言弹窗真的出现（抓"按钮在但点了没反应"）。
    // 标签通过 window.__LABEL__ 指定，缺省依次尝试常见文案。
    const labels = window.__LABEL__ ? [window.__LABEL__] : ["新建", "新建任务", "新建项目", "新建插件", "新建字典", "新建 API Key"];
    const btn = [...document.querySelectorAll("button")].filter(vis).find(b => labels.some(l => b.innerText.replace(/\s+/g, "") === l.replace(/\s+/g, "")));
    add("目标按钮存在", !!btn, btn ? btn.innerText.trim() : labels.join("|"));
    if (btn) {
      btn.click();
      await new Promise(r => setTimeout(r, 1800));
      const dlg = [...document.querySelectorAll(".el-dialog")].filter(d => d.getBoundingClientRect().height > 60)[0];
      add("弹窗已打开", !!dlg, dlg ? (dlg.querySelector(".el-dialog__title")?.innerText || "opened") : "not opened");
      if (dlg) {
        const cancel = [...dlg.querySelectorAll("button")].find(b => /取消|Cancel|关闭/.test(b.innerText));
        if (cancel) {
          cancel.click();
          await new Promise(r => setTimeout(r, 600));
        }
      }
    }
  } else if (mode === "resetWorks") {
    // 点「重置」后：URL query 清空、所有文本输入框为空、列表有数据或明确空态。
    // 这类断言专门抓「后加的筛选字段漏在重置之外」（2026-11-20 hostFilter 漏过一次）。
    const btn = [...document.querySelectorAll("button")].filter(vis).find(b => b.innerText.trim() === "重置");
    add("重置按钮存在", !!btn, btn ? "ok" : "missing");
    if (btn) {
      btn.click();
      await new Promise(r => setTimeout(r, 2200));
      add("URL 参数已清空", !location.hash.includes("?"), location.hash.slice(0, 70));
      const vals = [...document.querySelectorAll("input")].filter(vis).map(i => i.value).filter(v => v && v !== "on" && v !== "1");
      add("输入框已清空", vals.length === 0, vals.slice(0, 3).join("|") || "empty");
      add("列表有数据或空态", rows() > 0 || !!empties()[0], rows() + " 行");
    }
  } else if (mode === "hasButtons") {
    const b = buttons();
    add("保存按钮存在", b.some(x => x === "保存"), b.slice(0, 5).join("|"));
    add("页面已渲染", text.length > 300, text.length + " 字符");
  }

  return JSON.stringify({ mode, ok: checks.every(c => c.pass), checks });
})()