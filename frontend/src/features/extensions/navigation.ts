export interface ExtensionAdminNavItem {
  path: string
  label: string
  icon: unknown
  hideInSimpleMode?: boolean
  expandOnly?: boolean
  activePrefix?: string
  children?: ExtensionAdminNavItem[]
}

interface ExtensionNavigationIcons {
  ShieldIcon: unknown
  ChartIcon: unknown
  FolderIcon: unknown
}

export function createExtensionAdminNavItems({
  ShieldIcon,
  ChartIcon,
  FolderIcon
}: ExtensionNavigationIcons): ExtensionAdminNavItem[] {
  return [
    {
      path: '/admin/extensions',
      label: '扩展中心',
      icon: ShieldIcon,
      hideInSimpleMode: true,
      expandOnly: true,
      children: [
        {
          path: '/admin/extensions/user-risk/users',
          activePrefix: '/admin/extensions/user-risk',
          label: '用户风控',
          icon: ShieldIcon
        },
        {
          path: '/admin/extensions/account-monitor',
          label: '账号监控',
          icon: ChartIcon
        },
        {
          path: '/admin/extensions/group-monitor',
          label: '分组监控',
          icon: FolderIcon
        }
      ]
    }
  ]
}
