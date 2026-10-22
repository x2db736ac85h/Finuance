// The legacy `.modal` window: a dimmed backdrop with a `.modal-card`, a header with the title and a
// close button. Focus stays inside while it is open; Esc and a click on the backdrop close it.

import { FloatingFocusManager, FloatingPortal, useDismiss, useFloating, useInteractions } from '@floating-ui/react';
import { useCallback, type ReactNode } from 'react';

import { IconButton } from './IconButton';
import { useLayer } from './layers';

type Props = {
  open: boolean;
  onClose: () => void;
  title: string;
  sub?: ReactNode;
  size?: 'sm';
  /** Extra header controls, before the close button. */
  actions?: ReactNode;
  children: ReactNode;
};

export function Modal({ open, onClose, title, sub, size, actions, children }: Props) {
  const { refs, context } = useFloating({
    open,
    onOpenChange: (o) => {
      if (!o) onClose();
    },
  });
  const { getFloatingProps } = useInteractions([useDismiss(context, { outsidePress: false })]);
  const setFloating = useCallback(
    (el: HTMLElement | null) => {
      refs.setFloating(el);
    },
    [refs],
  );
  useLayer('modal', open);
  if (!open) return null;
  return (
    <FloatingPortal>
      <div
        className="modal"
        onMouseDown={(e) => {
          if (e.target === e.currentTarget) onClose();
        }}
      >
        <FloatingFocusManager context={context} initialFocus={-1}>
          <div
            ref={setFloating}
            className={size ? `modal-card ${size}` : 'modal-card'}
            role="dialog"
            aria-modal="true"
            aria-label={title}
            tabIndex={-1}
            {...getFloatingProps()}
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
      </div>
    </FloatingPortal>
  );
}
