const { contextBridge, ipcRenderer, webFrame } = require('electron');

const bridge = {
  isElectron: true,
  minimize: () => ipcRenderer.send('window-minimize'),
  maximize: () => ipcRenderer.send('window-maximize'),
  close: () => ipcRenderer.send('window-close'),
  setZoom: (factor) => webFrame.setZoomFactor(factor),
  pickDirectory: () => Promise.resolve(null),
  listDir: () =>
    Promise.resolve({
      path: '',
      parent: null,
      entries: [],
      home: '',
    }),
  agent: {
    send: () => {},
    setCwd: () => {},
    onEvent: () => () => {},
  },
};

contextBridge.exposeInMainWorld('enoughIPC', bridge);
contextBridge.exposeInMainWorld('flame', bridge);
