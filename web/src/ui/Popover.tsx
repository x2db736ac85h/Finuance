// The legacy `#pop` popover (PLAN §7.6) on Floating UI: anchored below its button, flips and shifts
// to stay on screen, re-anchors on scroll and resize, and closes on outside click or Esc with focus
// back on the anchor. Widths are logical pixels, scaled with the interface size.

import {
  autoUpdate,
  flip,
  FloatingFocusManager,
  FloatingPortal,
  offset,
  shift,
  size,
  useDismiss,
  useFloating,
  useInteractions,
  useRole,
} from '@floating-ui/react';
import { useCallback, useState, type KeyboardEvent, type MouseEvent, type ReactNode } from 'react';

import { uiScale } from '../styles/scale';
import { useLayer } from './layers';

type Props = {
  open: boolean;
  onClose: () => void;
  anchor: Element | null;
  /** aria-label of the popover. */
  label: string;
  /** Logical width in px (legacy openPop width). */
  width?: number;
  role?: 'dialog' | 'menu';
  className?: string;
  onKeyDown?: (e: KeyboardEvent<HTMLDivElement>) => void;
  children: ReactNode;
};

export function Popover({
  open,
  onClose,
  anchor,
  label,
  width = 300,
  role = 'dialog',
  className,
  onKeyDown,
  children,
}: Props) {
  const { refs, floatingStyles, context } = useFloating({
    open,
    onOpenChange: (o) => {
      if (!o) onClose();
    },
    elements: { reference: anchor },
    placement: 'bottom-start',
    strategy: 'fixed',
    middleware: [
      offset(6),
      flip({ padding: 8 }),
      shift({ padding: 8 }),
      size({
        padding: 8,
        apply({ availableHeight, elements }) {
          elements.floating.style.maxHeight = `min(37.5rem, 84vh, ${Math.max(120, availableHeight)}px)`;
        },
      }),
    ],
    whileElementsMounted: autoUpdate,
  });
  const { getFloatingProps } = useInteractions([useDismiss(context), useRole(context, { role })]);
  const setFloating = useCallback(
    (el: HTMLElement | null) => {
      refs.setFloating(el);
    },
    [refs],
  );
  useLayer('pop', open);
  if (!open) return null;
  const w = Math.min(width * uiScale(), window.innerWidth - 16);
  return (
    <FloatingPortal>
      <FloatingFocusManager context={context} modal={false} initialFocus={role === 'menu' ? 0 : -1}>
        <div
          ref={setFloating}
          className={className ? `pop ${className}` : 'pop'}
          style={{ ...floatingStyles, width: w }}
          aria-label={label}
          {...getFloatingProps({ onKeyDown })}
        >
          {children}
        </div>
      </FloatingFocusManager>
    </FloatingPortal>
  );
}

/** Open/close state for a popover anchored at the button that opened it. */
export function usePopover() {
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const toggle = useCallback((e: MouseEvent<HTMLElement>) => {
    const el = e.currentTarget;
    setAnchor((a) => (a ? null : el));
  }, []);
  const close = useCallback(() => {
    setAnchor(null);
  }, []);
  return { open: anchor !== null, anchor, toggle, close };
}
