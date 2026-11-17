// 「未分组」在列表接口里是哨兵值（后端按 group_id ∈ ["", "0"] 过滤），
// 在写接口里必须还原成空串：后端 ensureGroup 会把任何非 ObjectID 的 groupId
// 当成非法集团 id 直接 400，所以传 'unassigned' 落库是错的。
export const UNASSIGNED_GROUP_ID = 'unassigned'

export const resolveGroupIdForWrite = (groupId?: string): string =>
  !groupId || groupId === UNASSIGNED_GROUP_ID ? '' : groupId
