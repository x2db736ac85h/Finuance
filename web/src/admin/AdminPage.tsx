// /admin/:section (PLAN Phase 19, ADR 0025): the instance's administration, for administrators
// only. It shows accounts and counts, never anyone's financial data. A lazy chunk: most users never
// load it.

import { Navigate, NavLink, useOutletContext, useParams } from 'react-router';

import type { ShellContext } from '../shell/AppShell';
import { Icon } from '../ui/Icon';
import type { IconName } from '../ui/icons';
import { InstanceSettings } from './Instance';
import { ThemesAdmin } from './Themes';
import { UsageAdmin } from './Usage';
import { UsersAdmin } from './Users';

const SECTIONS = [
  { id: 'users', label: 'Users', icon: 'pen', Body: UsersAdmin },
  { id: 'instance', label: 'Defaults and features', icon: 'sliders', Body: InstanceSettings },
  { id: 'themes', label: 'Colour themes', icon: 'sun', Body: ThemesAdmin },
  { id: 'usage', label: 'Usage', icon: 'chart', Body: UsageAdmin },
] as const satisfies readonly { id: string; label: string; icon: IconName; Body: () => React.JSX.Element }[];

export function AdminPage() {
  const { user } = useOutletContext<ShellContext>();
  const { section } = useParams();
  if (!user.isAdmin) return <Navigate to="/" replace />;
  const current = SECTIONS.find((s) => s.id === section);
  if (!current) return <Navigate to="/admin/users" replace />;
  const Body = current.Body;
  return (
    <div className="settings">
      <nav className="set-nav" aria-label="Administration">
        <div className="sec-h">Administration</div>
        {SECTIONS.map((s) => (
          <NavLink key={s.id} to={`/admin/${s.id}`} className={({ isActive }) => (isActive ? 'mi on' : 'mi')}>
            <Icon name={s.icon} />
            {s.label}
          </NavLink>
        ))}
        <NavLink to="/" className="mi">
          <Icon name="left" />
          Back to transactions
        </NavLink>
      </nav>
      <div className="set-body adm-body">
        <h1>{current.label}</h1>
        <Body />
      </div>
    </div>
  );
}
