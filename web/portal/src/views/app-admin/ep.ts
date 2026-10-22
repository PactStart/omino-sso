/**
 * 门户应用管理页的 Element Plus 局部引入（006-app-admin-role）。
 *
 * 约束（AGENTS.md §2.2）：只有 `views/app-admin/` 下的组件可以 import 本模块；
 * `portal/src/main.ts` 不注册 Element Plus，登录页/账号中心/我的应用首屏不得加载 EP。
 * 由 Vite 按路由切成独立 chunk（含 `element-plus/dist/index.css`）。
 */
import 'element-plus/dist/index.css'

export {
  ElAlert,
  ElButton,
  ElDescriptions,
  ElDescriptionsItem,
  ElDialog,
  ElEmpty,
  ElForm,
  ElFormItem,
  ElInput,
  ElInputNumber,
  ElLoading,
  ElMessage,
  ElMessageBox,
  ElPagination,
  ElRadioButton,
  ElRadioGroup,
  ElSwitch,
  ElTabPane,
  ElTabs,
  ElTable,
  ElTableColumn,
  ElTag,
  ElTree,
  ElTreeSelect,
} from 'element-plus'

export type { FormInstance, FormRules } from 'element-plus'
