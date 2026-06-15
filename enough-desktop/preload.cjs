const { contextBridge, ipcRenderer, webFrame } = require('electron');

const bridge = {
  isElectron: true,
  minimize: () => ipcRenderer.send('window-minimize'),
  maximize: () => ipcRenderer.send('window-maximize'),
  close: () => ipcRenderer.send('window-close'),
  setZoom: (factor) => webFrame.setZoomFactor(factor),
  pickDirectory: () => ipcRenderer.invoke('fs-pick-directory'),
  listDir: (targetPath) => ipcRenderer.invoke('fs-list-dir', targetPath),
};

contextBridge.exposeInMainWorld('enoughIPC', bridge);
contextBridge.exposeInMainWorld('enough', bridge);
