const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('api', {
  getCaptureSources: () => ipcRenderer.invoke('get-capture-sources'),
  analyzeScreen: (imageBase64) => ipcRenderer.invoke('analyze-screen', imageBase64),
  notify: (title, body) => ipcRenderer.invoke('notify', { title, body }),
});
