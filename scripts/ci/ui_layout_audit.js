// 页面布局体检脚本：在浏览器控制台（或 Agent Bridge 的 eval）里跑，返回当前页面的布局问题。
//
// 用法：
//   1) 打开目标页，等数据加载完；
//   2) 把整个文件内容包成 (function(){...})() 求值，或直接粘贴到 Console；
//   3) 看返回的 JSON：issues 里每条带 kind，按 kind 决定要不要修。
//
// 三类判定：
//   h-overflow  整页出现横向滚动（某处宽度写死/负 margin 常见）
//   wrapped     同一工具栏里的控件被挤到第二行 —— 注意用「顶边 > 第一行最大底边」判定，
//               不能只看 top 是否相等：同一行里按钮/输入框高度不同，top 天然不一致（会假阳性）
//   zero-btn    可点击元素被压成 0 尺寸（span/width 组合写错时的典型症状）
//   stuck-mask  卡在离场过渡里的 el-loading 遮罩（opacity:0 但仍吃点击 → 整页点不动）
//
// 踩过的坑（2026-11-28）：任务页筛选栏曾被挤成两行，改了三种写法都没生效 ——
//   span 无效（CSS 已把 .el-col 覆盖成 flex:0 0 auto）、scoped 类被同权重规则压住、
//   行内 :style 仍被 ElCol 自身按 span 计算的 flex 覆盖。最终改成普通 div + flex 容器。
//   结论：el-col 不适合做「按内容定宽」的工具栏，直接用它排版会打架。
(() => {
  // node 下没有 document：直接给人用法提示，而不是抛 ReferenceError（踩过：以为能 node 跑）
  if (typeof document === 'undefined') {
    const msg =
      'ui_layout_audit: 这是浏览器脚本。请在页面 Console 里粘贴，或用 Agent Bridge 的 eval 求值（node 下无 document）'
    if (typeof console !== 'undefined') console.log(msg)
    return msg
  }
  // 还在加载就别量：el-loading 期间表格/卡片是占位尺寸，量出来全是「换行」假阳
  // （2026-11-28 实测：资产页刚切过去跑出 6 条 wrapped，4 秒后同页 0 条）
  const mask = [...document.querySelectorAll('.el-loading-mask')].some((m) => m.offsetParent)
  if (mask) return JSON.stringify({ url: location.hash, loading: true, total: 0, issues: [] })
  const issues = [];
  const de = document.documentElement;
  if (de.scrollWidth > window.innerWidth + 2) {
    issues.push({ kind: 'h-overflow', detail: de.scrollWidth + ' > ' + window.innerWidth });
  }
  for (const row of document.querySelectorAll('.task-filters, .task-toolbar, .mb-10px, .el-row')) {
    const kids = [...row.children].filter((k) => k.offsetParent);
    if (kids.length < 3) continue;
    // el-row 里各列 span 之和 > 24 表示这一行本来就该折行（登录表单 24+24+24+24 就是），
    // 不是缺陷。只有「本来就该排一行」却折了才算。span 读不出来时按旧逻辑判。
    if (row.classList.contains('el-row')) {
      const spans = kids.map((k) => {
        const m = String(k.className).match(/\bel-col-(\d+)\b/)
        return m ? Number(m[1]) : null
      })
      if (spans.every((v) => v !== null) && spans.reduce((a, b) => a + b, 0) > 24) continue
    }
    const rects = kids.map((k) => k.getBoundingClientRect());
    const minTop = Math.min(...rects.map((r) => r.top));
    const tallest = Math.max(...rects.map((r) => r.height));
    if (rects.some((r) => r.top > minTop + tallest + 4)) {
      issues.push({ kind: 'wrapped', sel: String(row.className).slice(0, 40), spread: Math.round(Math.max(...rects.map((r) => r.bottom)) - minTop) });
    }
  }
  // 卡在离场过渡里的 loading 遮罩：opacity 已经是 0，元素却还在吃指针事件，
  // 于是「页面看着正常、点哪都没反应」。用 elementFromPoint 判定，别只看 opacity。
  for (const m of document.querySelectorAll('.el-loading-mask')) {
    const cs = getComputedStyle(m)
    if (cs.display === 'none' || cs.visibility === 'hidden') continue
    if (parseFloat(cs.opacity || '1') > 0.02) continue
    // 注意：position:fixed 的元素 offsetParent 恒为 null，别用它判可见
    const r = m.getBoundingClientRect()
    if (r.width < 8 || r.height < 8) continue
    const hit = document.elementFromPoint(Math.round(r.left + r.width / 2), Math.round(r.top + r.height / 2))
    if (hit && hit.closest && hit.closest('.el-loading-mask') === m) {
      issues.push({ kind: 'stuck-mask', sel: String(m.className).slice(0, 50), w: Math.round(r.width), h: Math.round(r.height) })
    }
  }
  for (const b of document.querySelectorAll('button, .el-button')) {
    if (!b.offsetParent) continue;
    const r = b.getBoundingClientRect();
    if (r.width < 8 || r.height < 8) {
      issues.push({ kind: 'zero-btn', text: (b.innerText || '').trim().slice(0, 16), w: Math.round(r.width), h: Math.round(r.height) });
    }
  }
  return JSON.stringify({ url: location.hash, total: issues.length, issues: issues.slice(0, 15) });
})()