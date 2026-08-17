import type { ApiAuthSession } from './api'

/**
 * 当前登录的人有没有某个权限。
 *
 * **这不是安全边界**，后端每个接口自己会拦。它存在只为一件事：把「按下去才知道
 * 不行」变成「按之前就说清为什么不行」。少了这一层，一个 member 会填完改阈值的
 * 理由、按下保存，然后收到一句 403 —— 而他做错的事在打开这一页时就已经注定了。
 */
export function hasScope(session: ApiAuthSession, scope: string): boolean {
  return (session.scopes ?? []).includes(scope)
}

/** 组织角色的中文名。角色是权限的唯一来源，所以哪里说权限，哪里就要能说出角色。 */
export const organizationRoleLabel: Record<string, string> = {
  owner: '组织所有者',
  admin: '管理员',
  member: '成员',
  auditor: '审计',
}

/**
 * 洞察这三档权限，当前登录的人有哪几档。
 *
 * 只讲 insights 这三个：一屏说的就是它们，把 delivery.execute 之类一起列出来，
 * 人反而找不到自己关心的那一行。
 */
export function insightScopeSummary(scopes: string[] | undefined): string {
  const owned = ['insights.read', 'insights.write', 'insights.confirm']
    .filter(scope => (scopes ?? []).includes(scope))
    .map(scope => ({ 'insights.read': '读取', 'insights.write': '编辑', 'insights.confirm': '确认' })[scope])
  return owned.length ? owned.join(' + ') : '连读取都没有（这一页多半也打不开）'
}

/**
 * 「你现在是<角色>」这半句话。
 *
 * 角色读不到时不能硬凑一个词——原来兜底写的是「当前角色」，于是界面上出现
 * 「你现在是当前角色」这种谁也看不懂的句子，读的人会以为是页面出了故障。
 * 读不到就直说读不到，并把真正管事的那一项（有哪几档权限）摆出来。
 */
export function roleSentence(role: string | undefined, scopes: string[] | undefined): string {
  const label = organizationRoleLabel[role ?? '']
  return label
    ? `你现在是${label}，有${insightScopeSummary(scopes)}`
    : `没读到你的组织角色（组织信息这次没取到），按权限看你有${insightScopeSummary(scopes)}`
}
