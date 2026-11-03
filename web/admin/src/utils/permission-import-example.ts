/** 权限点导入示例文档（011-app-permission-import）。对话框与权限页头部共用同一份。 */
import { downloadJson } from '@/utils/file'

/** 3 层 5 条，覆盖 version / mode / children / sort / description 的写法。 */
export const PERMISSION_IMPORT_EXAMPLE = {
  version: 1,
  mode: 'merge',
  permissions: [
    {
      code: 'order',
      name: '订单',
      description: '订单域',
      sort: 0,
      children: [
        {
          code: 'order.read',
          name: '查询订单',
          children: [
            { code: 'order.read.self', name: '查询本人订单' },
            { code: 'order.read.all', name: '查询全部订单' },
          ],
        },
        { code: 'order.write', name: '创建订单', sort: 1 },
      ],
    },
  ],
}

/** 下载示例文档（文件名固定，便于管理员反复对照）。 */
export function downloadPermissionImportExample(): void {
  downloadJson('permissions-example.json', PERMISSION_IMPORT_EXAMPLE)
}
