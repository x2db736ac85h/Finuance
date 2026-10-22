import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { describe, expect, it, vi } from 'vitest';

import { Button } from './Button';
import { IconButton } from './IconButton';
import { useLayers } from './layers';
import { Menu, MenuItem } from './Menu';
import { Modal } from './Modal';
import { Popover, usePopover } from './Popover';
import { Toaster } from './Toast';
import { toast, useToasts } from './toastStore';

function PopoverDemo() {
  const p = usePopover();
  return (
    <>
      <Button onClick={p.toggle}>Open</Button>
      <span>outside</span>
      <Popover open={p.open} anchor={p.anchor} onClose={p.close} label="Demo">
        <input aria-label="inside" />
      </Popover>
    </>
  );
}

function MenuDemo({ onPick }: { onPick: (s: string) => void }) {
  const p = usePopover();
  return (
    <>
      <Button onClick={p.toggle}>Menu</Button>
      <Menu open={p.open} anchor={p.anchor} onClose={p.close} label="Actions">
        {['One', 'Two', 'Three'].map((x) => (
          <MenuItem
            key={x}
            onSelect={() => {
              onPick(x);
              p.close();
            }}
          >
            {x}
          </MenuItem>
        ))}
      </Menu>
    </>
  );
}

function ModalDemo() {
  const [open, setOpen] = useState(true);
  return (
    <Modal
      open={open}
      onClose={() => {
        setOpen(false);
      }}
      title="Window"
    >
      <p>body</p>
    </Modal>
  );
}

describe('Popover', () => {
  it('opens at its button, registers a layer and closes on Esc and outside click', async () => {
    const user = userEvent.setup();
    render(<PopoverDemo />);
    await user.click(screen.getByRole('button', { name: 'Open' }));
    expect(screen.getByRole('dialog', { name: 'Demo' }).className).toBe('pop');
    expect(useLayers.getState().pop).toBe(1);
    await user.keyboard('{Escape}');
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
    expect(useLayers.getState().pop).toBe(0);
    await user.click(screen.getByRole('button', { name: 'Open' }));
    await user.click(screen.getByText('outside'));
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
  });

  it('toggles from its own button', async () => {
    const user = userEvent.setup();
    render(<PopoverDemo />);
    const btn = screen.getByRole('button', { name: 'Open' });
    await user.click(btn);
    await user.click(btn);
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
  });
});

describe('Menu', () => {
  it('is operable with the arrow keys and Enter', async () => {
    const pick = vi.fn();
    const user = userEvent.setup();
    render(<MenuDemo onPick={pick} />);
    await user.click(screen.getByRole('button', { name: 'Menu' }));
    await waitFor(() => {
      expect(document.activeElement?.textContent).toBe('One');
    });
    await user.keyboard('{ArrowDown}{ArrowDown}{ArrowDown}');
    expect(document.activeElement?.textContent).toBe('One');
    await user.keyboard('{ArrowUp}');
    expect(document.activeElement?.textContent).toBe('Three');
    await user.keyboard('{Enter}');
    expect(pick).toHaveBeenCalledWith('Three');
  });
});

describe('Modal', () => {
  it('closes with Esc', async () => {
    const user = userEvent.setup();
    render(<ModalDemo />);
    expect(screen.getByRole('dialog', { name: 'Window' })).toBeTruthy();
    expect(useLayers.getState().modal).toBe(1);
    await user.keyboard('{Escape}');
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
    expect(useLayers.getState().modal).toBe(0);
  });

  it('closes on a backdrop click but not on a click inside', async () => {
    const user = userEvent.setup();
    render(<ModalDemo />);
    await user.click(screen.getByText('body'));
    expect(screen.getByRole('dialog')).toBeTruthy();
    const backdrop = document.querySelector('.modal');
    if (!backdrop) throw new Error('no backdrop');
    await user.click(backdrop);
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
  });
});

describe('Toast', () => {
  it('shows queued toasts one after another', () => {
    vi.useFakeTimers();
    try {
      render(<Toaster />);
      act(() => {
        toast('First');
        toast('First'); // repeated: shown once
        toast('Second', { kind: 'error' });
      });
      expect(useToasts.getState().queue).toHaveLength(2);
      act(() => {
        vi.advanceTimersByTime(10);
      });
      expect(screen.getByRole('status').textContent).toBe('First');
      expect(screen.getByRole('status').className).toBe('toast on');
      act(() => {
        vi.advanceTimersByTime(2200 + 180);
      });
      act(() => {
        vi.advanceTimersByTime(10);
      });
      const err = screen.getByRole('alert');
      expect(err.textContent).toBe('Second');
      expect(err.className).toBe('toast err on');
    } finally {
      vi.useRealTimers();
    }
  });
});

describe('IconButton', () => {
  it('uses its title as the accessible name', () => {
    render(<IconButton icon="x" title="Close" on />);
    const b = screen.getByRole('button', { name: 'Close' });
    expect(b.className).toBe('ib on');
  });
});

describe('Toast actions', () => {
  it('runs an action and closes the toast', async () => {
    const undo = vi.fn();
    const user = userEvent.setup();
    render(<Toaster />);
    act(() => {
      toast('Imported 12 transactions', { actions: [{ label: 'Undo', run: undo }] });
    });
    const btn = await screen.findByRole('button', { name: 'Undo' });
    expect(screen.getByRole('status').className).toContain('act');
    await user.click(btn);
    expect(undo).toHaveBeenCalledTimes(1);
    await waitFor(() => {
      expect(useToasts.getState().queue).toHaveLength(0);
    });
  });
});
