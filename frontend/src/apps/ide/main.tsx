import React from 'react';
import ReactDOM from 'react-dom/client';

import '../../config/monacoSetup';
import '../../config/monacoLanguages';
import { IDEApp } from './IDEApp';
import '../../styles/ide.css';

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <IDEApp />
  </React.StrictMode>,
);
