const {
  app,
  BrowserWindow,
  desktopCapturer,
  ipcMain,
  Notification,
  globalShortcut,
  dialog,
  safeStorage,
} = require('electron');
const path = require('path');
const fs = require('fs');
const { analyzeScreenshot, summarizeSession, setApiKey, hasApiKey } = require('./analyzer');

const HOTKEY = 'CommandOrControl+Shift+R';
let mainWindow;
let configPath;

function loadStoredKey() {
  try {
    const cfg = JSON.parse(fs.readFileSync(configPath, 'utf8'));
    if (cfg.apiKeyEnc && safeStorage.isEncryptionAvailable()) {
      return safeStorage.decryptString(Buffer.from(cfg.apiKeyEnc, 'base64'));
    }
    return cfg.apiKey || '';
  } catch {
    return '';
  }
}

function storeKey(key) {
  let cfg;
  if (safeStorage.isEncryptionAvailable()) {
    cfg = { apiKeyEnc: safeStorage.encryptString(key).toString('base64') };
  } else {
    cfg = { apiKey: key };
  }
  fs.writeFileSync(configPath, JSON.stringify(cfg, null, 2));
}

function createWindow() {
  mainWindow = new BrowserWindow({
    width: 1040,
    height: 940,
    backgroundColor: '#0e1116',
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      nodeIntegration: false,
    },
  });
  mainWindow.loadFile(path.join(__dirname, 'renderer', 'index.html'));
}

app.whenReady().then(() => {
  configPath = path.join(app.getPath('userData'), 'config.json');
  const storedKey = loadStoredKey();
  if (storedKey) setApiKey(storedKey);

  ipcMain.handle('get-status', () => ({ hasKey: hasApiKey() }));

  ipcMain.handle('set-api-key', (_event, key) => {
    setApiKey(key);
    try {
      storeKey(key);
    } catch (err) {
      return { ok: false, error: err.message };
    }
    return { ok: true, hasKey: hasApiKey() };
  });

  ipcMain.handle('get-capture-sources', async () => {
    const sources = await desktopCapturer.getSources({
      types: ['window', 'screen'],
      thumbnailSize: { width: 0, height: 0 },
    });
    return sources.map((s) => ({
      id: s.id,
      name: s.name,
      kind: s.id.startsWith('screen') ? 'screen' : 'window',
    }));
  });

  ipcMain.handle('analyze-screen', (_event, imageBase64) => analyzeScreenshot(imageBase64));

  ipcMain.handle('summarize-session', (_event, summaries) => summarizeSession(summaries));

  ipcMain.handle('save-recording', async (_event, bytes) => {
    const defaultPath = path.join(app.getPath('videos'), `recording-${Date.now()}.webm`);
    const { canceled, filePath } = await dialog.showSaveDialog(mainWindow, {
      title: 'Save recording',
      defaultPath,
      filters: [{ name: 'WebM video', extensions: ['webm'] }],
    });
    if (canceled || !filePath) return { ok: false, canceled: true };
    fs.writeFileSync(filePath, Buffer.from(bytes));
    return { ok: true, path: filePath };
  });

  ipcMain.handle('notify', (_event, { title, body }) => {
    new Notification({ title, body }).show();
  });

  const registered = globalShortcut.register(HOTKEY, () => {
    if (mainWindow) mainWindow.webContents.send('hotkey-toggle');
  });

  createWindow();

  if (!registered && mainWindow) {
    mainWindow.webContents.once('did-finish-load', () => {
      mainWindow.webContents.send('hotkey-failed');
    });
  }

  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow();
  });
});

app.on('will-quit', () => globalShortcut.unregisterAll());

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') app.quit();
});
