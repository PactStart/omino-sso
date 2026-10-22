import type { InjectionKey } from 'vue'

/**
 * 子页面保存成功后，通知外层 ClientDetail 重新拉取头部摘要
 * （应用名称 / 启用状态 / 成员数），使页面内各处信息立即一致（FR-004）。
 */
export const reloadClientSummaryKey: InjectionKey<() => void | Promise<void>> =
  Symbol('reloadClientSummary')
