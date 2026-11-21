import './styles/tokens.css';
import './styles/base.css';
import './styles/app.css';
import './styles/finuance.css';

import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

import { App } from './App';
import { installClientEventHandlers } from './lib/clientEvents';

installClientEventHandlers(); // uncaught errors are counted on the server (PLAN 24.8)

const root = document.getElementById('root');
if (!root) throw new Error('#root is missing from index.html');
createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
