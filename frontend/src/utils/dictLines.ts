// 字典文本切分：一行一条，去掉首尾空白与空行，并保序去重。
//
// 与后端 NormalizeLine（TrimSpace + 丢弃空行）和 NormalizeLines（保序去重）同口径，
// 这样「追加时提示会去重」与实际入库结果一致：前端先去重，后端再兜一次。
export const splitDictLines = (text: string): string[] => {
  const seen = new Set<string>()
  const lines: string[] = []
  for (const raw of text.split(/\r\n|\r|\n/)) {
    const line = raw.trim()
    if (!line || seen.has(line)) continue
    seen.add(line)
    lines.push(line)
  }
  return lines
}
