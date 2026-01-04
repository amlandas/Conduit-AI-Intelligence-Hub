import { app, Menu, shell, BrowserWindow } from 'electron'

export function createApplicationMenu(): void {
  const isMac = process.platform === 'darwin'

  const template: Electron.MenuItemConstructorOptions[] = [
    ...(isMac
      ? [
          {
            label: app.name,
            submenu: [
              { role: 'about' as const },
              { type: 'separator' as const },
              {
                label: 'Preferences...',
                accelerator: 'Cmd+,',
                click: (): void => {
                  const window = BrowserWindow.getFocusedWindow()
                  window?.webContents.send('navigate', '/settings')
                }
              },
              { type: 'separator' as const },
              { role: 'services' as const },
              { type: 'separator' as const },
              { role: 'hide' as const },
              { role: 'hideOthers' as const },
              { role: 'unhide' as const },
              { type: 'separator' as const },
              { role: 'quit' as const }
            ]
          }
        ]
      : []),
    {
      label: 'File',
      submenu: [isMac ? { role: 'close' as const } : { role: 'quit' as const }]
    },
    {
      label: 'Edit',
      submenu: [
        { role: 'undo' as const },
        { role: 'redo' as const },
        { type: 'separator' as const },
        { role: 'cut' as const },
        { role: 'copy' as const },
        { role: 'paste' as const },
        ...(isMac
          ? [
              { role: 'pasteAndMatchStyle' as const },
              { role: 'delete' as const },
              { role: 'selectAll' as const }
            ]
          : [{ role: 'delete' as const }, { type: 'separator' as const }, { role: 'selectAll' as const }])
      ]
    },
    {
      label: 'View',
      submenu: [
        { role: 'reload' as const },
        { role: 'forceReload' as const },
        { role: 'toggleDevTools' as const },
        { type: 'separator' as const },
        { role: 'resetZoom' as const },
        { role: 'zoomIn' as const },
        { role: 'zoomOut' as const },
        { type: 'separator' as const },
        { role: 'togglefullscreen' as const }
      ]
    },
    {
      label: 'Go',
      submenu: [
        {
          label: 'Dashboard',
          accelerator: 'Cmd+1',
          click: (): void => {
            const window = BrowserWindow.getFocusedWindow()
            window?.webContents.send('navigate', '/')
          }
        },
        {
          label: 'Knowledge Base',
          accelerator: 'Cmd+2',
          click: (): void => {
            const window = BrowserWindow.getFocusedWindow()
            window?.webContents.send('navigate', '/kb')
          }
        },
        {
          label: 'Connectors',
          accelerator: 'Cmd+3',
          click: (): void => {
            const window = BrowserWindow.getFocusedWindow()
            window?.webContents.send('navigate', '/connectors')
          }
        },
        {
          label: 'Settings',
          accelerator: 'Cmd+4',
          click: (): void => {
            const window = BrowserWindow.getFocusedWindow()
            window?.webContents.send('navigate', '/settings')
          }
        },
        { type: 'separator' as const },
        {
          label: 'Search...',
          accelerator: 'Cmd+K',
          click: (): void => {
            const window = BrowserWindow.getFocusedWindow()
            window?.webContents.send('open-search')
          }
        }
      ]
    },
    {
      label: 'Window',
      submenu: [
        { role: 'minimize' as const },
        { role: 'zoom' as const },
        ...(isMac
          ? [{ type: 'separator' as const }, { role: 'front' as const }, { type: 'separator' as const }, { role: 'window' as const }]
          : [{ role: 'close' as const }])
      ]
    },
    {
      role: 'help' as const,
      submenu: [
        {
          label: 'Documentation',
          click: async (): Promise<void> => {
            await shell.openExternal('https://github.com/simpleflo/conduit')
          }
        },
        {
          label: 'Report Issue',
          click: async (): Promise<void> => {
            await shell.openExternal('https://github.com/simpleflo/conduit/issues')
          }
        }
      ]
    }
  ]

  const menu = Menu.buildFromTemplate(template)
  Menu.setApplicationMenu(menu)
}
