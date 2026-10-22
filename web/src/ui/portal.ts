import { createContext } from 'react';

/**
 * Where popovers render. Inside a modal it is the modal's overlay, so they stay inside its focus
 * trap and above it; elsewhere it is the document body (null).
 */
export const PortalRoot = createContext<HTMLElement | null>(null);
