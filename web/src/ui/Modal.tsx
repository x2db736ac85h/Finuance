// The legacy `.modal` window: a dimmed backdrop with a `.modal-card`, a header with the title and a
// close button. Focus stays inside while it is open. Esc closes it unless a popover inside is open
// (that popover closes first); a click on the backdrop closes it unless dismissOnBackdrop is false.

import { FloatingFocusManager, FloatingPortal, useFloating } from '@floating-ui/react';
import { useCallback, useEffect, useEffectEvent, useState, type ReactNode } from 'react';

import { IconButton } from './IconButton';
import { useLayer, useLayers } from './layers';
import { PortalRoot } from './portal';

type Props = {
  open: boolean;
  onClose: () => void;
  title: string;
  sub?: ReactNode;
  /** sm: a dialog; md: a window sized to its content (at most 50 rem wide); none: the big window. */
  size?: 'sm' | 'md';
  /** Extra header controls, before the close button. */
  actions?: ReactNode;
  dismissOnBackdrop?: boolean;
  children: ReactNode;
};

export function Modal({ open, onClose, title, sub, size, actions, dismissOnBackdrop = true, children }: Props) {
  const { refs, context } = useFloating({
    open,
    onOpenChange: (o) => {
      if (!o) onClose();
    },
  });
  const [overlay, setOverlay] = useState<HTMLDivElement | null>(null);
  const setFloating = useCallback(
    (el: HTMLElement | null) => {
      refs.setFloating(el);
    },
    [refs],
  );
  useLayer('modal', open);
  const escape = useEffectEvent((e: KeyboardEvent) => {
    if (e.key !== 'Escape' || useLayers.getState().pop > 0) return;
    e.preventDefault();
    onClose();
  });
  useEffect(() => {
    if (!open) return;
    const listener = (e: KeyboardEvent) => {
      escape(e);
    };
    document.addEventListener('keydown', listener);
    return () => {
      document.removeEventListener('keydown', listener);
    };
  }, [open]);
  if (!open) return null;
  return (
    <FloatingPortal>
      <div
        ref={setOverlay}
        className="modal"
        onMouseDown={(e) => {
          if (dismissOnBackdrop && e.target === e.currentTarget) onClose();
        }}
      >
        <PortalRoot value={overlay}>
          <FloatingFocusManager context={context} initialFocus={-1}>
            <div
              ref={setFloating}
              className={size ? `modal-card ${size}` : 'modal-card'}
              role="dialog"
              aria-modal="true"
              aria-label={title}
              tabIndex={-1}
            >
              <div className="modal-h">
                <b>{title}</b>
                {sub !== undefined && <span className="sub">{sub}</span>}
                <span className="spacer" />
                {actions}
                <IconButton icon="x" title="Close (Esc)" onClick={onClose} />
              </div>
              {children}
            </div>
          </FloatingFocusManager>
        </PortalRoot>
      </div>
    </FloatingPortal>
  );
}
