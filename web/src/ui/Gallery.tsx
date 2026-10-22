// Dev-only gallery of the UI primitives (PLAN task 8.5), at /__ui. Not part of production builds.

import { useState } from 'react';

import { AppearanceControls } from '../settings/Appearance';
import { useAppearance } from '../shell/effects';
import { Button } from './Button';
import { Icon } from './Icon';
import { IconButton } from './IconButton';
import { ICONS, type IconName } from './icons';
import { Checkbox, Select, TextInput } from './inputs';
import { Kbd, MOD } from './Kbd';
import { Logo } from './Logo';
import { Menu, MenuItem } from './Menu';
import { Modal } from './Modal';
import { Popover, usePopover } from './Popover';
import { Toaster } from './Toast';
import { toast } from './toastStore';
import { TooltipLayer, useTip } from './Tooltip';

export function Gallery() {
  useAppearance();
  const pop = usePopover();
  const menu = usePopover();
  const [modal, setModal] = useState(false);
  const [checked, setChecked] = useState(true);
  const [sel, setSel] = useState<'a' | 'b'>('a');
  const tip = useTip(
    <>
      <div className="tt-h">March 2025</div>
      <div className="r">
        <span className="k">Spent</span>
        <span className="v">-1 234,56</span>
      </div>
    </>,
  );
  return (
    <main className="gallery">
      <h1>
        <Logo /> UI primitives
      </h1>
      <section>
        <h2>Appearance</h2>
        <div className="gal-box">
          <AppearanceControls />
        </div>
      </section>
      <section>
        <h2>Buttons</h2>
        <div className="row2">
          <Button>Default</Button>
          <Button variant="primary" icon="folder">
            Primary
          </Button>
          <Button variant="ghost">Ghost</Button>
          <Button variant="danger">Danger</Button>
          <Button size="sm" icon="download">
            Small
          </Button>
          <Button disabled>Disabled</Button>
          <button type="button" className="link">
            Link
          </button>
          <IconButton icon="sidebar" title="Icon button" />
          <IconButton icon="chart" title="Icon button, on" on />
          <IconButton icon="x" title="Small icon button" size="sm" />
        </div>
      </section>
      <section>
        <h2>Inputs</h2>
        <div className="gal-box">
          <TextInput placeholder="Text input" />
          <TextInput mono placeholder="Mono input" />
          <Select
            value={sel}
            onChange={setSel}
            options={[
              { value: 'a', label: 'Option A' },
              { value: 'b', label: 'Option B' },
            ]}
          />
          <Checkbox label="Checkbox" checked={checked} onChange={setChecked} />
          <p>
            <Kbd keys={[MOD, 'F']} /> <Kbd keys={['Shift', 'Enter']} />
          </p>
        </div>
      </section>
      <section>
        <h2>Overlays</h2>
        <div className="row2">
          <Button onClick={pop.toggle}>Popover</Button>
          <Button onClick={menu.toggle}>Menu</Button>
          <Button
            onClick={() => {
              setModal(true);
            }}
          >
            Modal
          </Button>
          <Button
            onClick={() => {
              toast('Imported 312 transactions');
            }}
          >
            Toast
          </Button>
          <Button
            onClick={() => {
              toast('This file has no date column. Choose the booking date column in the mapping.', { kind: 'error' });
            }}
          >
            Error toast
          </Button>
          <span className="btn" {...tip}>
            Hover for a tooltip
          </span>
        </div>
      </section>
      <section>
        <h2>Icons</h2>
        <div className="gal-icons">
          {(Object.keys(ICONS) as IconName[]).map((n) => (
            <span key={n} title={n}>
              <Icon name={n} />
              {n}
            </span>
          ))}
        </div>
      </section>
      <Popover open={pop.open} anchor={pop.anchor} onClose={pop.close} label="Example popover">
        <div className="pop-h">
          <Icon name="filter" />
          <b>Popover</b>
          <span className="muted">header</span>
        </div>
        <div className="pop-s">
          <div className="lbl">Section</div>
          <TextInput placeholder="Condition, e.g. >100" />
          <div className="hint">
            Hint text with <code>code</code>.
          </div>
        </div>
        <div className="pop-f">
          <Button size="sm" variant="ghost" onClick={pop.close}>
            Cancel
          </Button>
          <Button size="sm" variant="primary" onClick={pop.close}>
            Apply
          </Button>
        </div>
      </Popover>
      <Menu open={menu.open} anchor={menu.anchor} onClose={menu.close} label="Example menu" width={220}>
        <MenuItem icon="up" onSelect={menu.close}>
          Sort ascending
        </MenuItem>
        <MenuItem icon="down" onSelect={menu.close} on>
          Sort descending
        </MenuItem>
        <MenuItem icon="eyeoff" onSelect={menu.close} hint="H">
          Hide column
        </MenuItem>
      </Menu>
      <Modal
        open={modal}
        onClose={() => {
          setModal(false);
        }}
        title="Modal"
        sub="subtitle"
        size="sm"
      >
        <div className="keys-b">
          <p>Esc, the close button or a click on the backdrop closes it. Focus stays inside.</p>
          <TextInput placeholder="Focusable" />
        </div>
      </Modal>
      <Toaster />
      <TooltipLayer />
    </main>
  );
}
