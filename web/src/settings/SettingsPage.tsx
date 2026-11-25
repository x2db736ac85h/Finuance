// /settings/:section (PLAN §7.3, task 8.9): a section list on the left, the section on the right.

import { Navigate, NavLink, useOutletContext, useParams } from 'react-router';

import { useScrollToHash } from '../lib/useScrollToHash';
import type { ShellContext } from '../shell/AppShell';
import { Icon } from '../ui/Icon';
import { RulesSettings } from '../rules/RulesSettings';
import { ActivitySettings } from './Activity';
import { CategoriesSettings } from './Categories';
import { DataSettings } from './Data';
import { GeneralSettings } from './General';
import { ProfileSettings } from './Profile';
import { SECTIONS, type SectionId } from './sections';
import { SessionsSettings } from './Sessions';
import { SecuritySettings } from './Security';
import { TokensSettings } from './Tokens';

const BODY: Record<SectionId, () => React.JSX.Element> = {
  general: GeneralSettings,
  profile: ProfileSettings,
  // everything about who can get into the account, on one page (Phase 26, U3)
  security: () => (
    <>
      <SecuritySettings />
      <section id="sessions" className="set-part" aria-label="Sessions">
        <h2>Sessions</h2>
        <SessionsSettings />
      </section>
      <section id="tokens" className="set-part" aria-label="Access tokens">
        <h2>Access tokens</h2>
        <TokensSettings />
      </section>
    </>
  ),
  activity: ActivitySettings,
  categories: CategoriesSettings,
  rules: RulesSettings,
  data: DataSettings,
};

/** Sections that became part of another page in Phase 26; their old addresses still lead there. */
const MOVED: Record<string, { section: SectionId; hash: string }> = {
  appearance: { section: 'general', hash: '#appearance' },
  sessions: { section: 'security', hash: '#sessions' },
  tokens: { section: 'security', hash: '#tokens' },
};

export function SettingsPage() {
  const { user } = useOutletContext<ShellContext>();
  const { section } = useParams();
  const sections = SECTIONS;
  const current = sections.find((s) => s.id === section);
  useScrollToHash(section);
  const moved = section ? MOVED[section] : undefined;
  if (moved) return <Navigate to={{ pathname: `/settings/${moved.section}`, hash: moved.hash }} replace />;
  if (!current) return <Navigate to="/settings/general" replace />;
  const Body = BODY[current.id];
  return (
    <div className="settings">
      <nav className="set-nav" aria-label="Settings">
        <div className="sec-h">Settings</div>
        {sections.map((s) => (
          <NavLink key={s.id} to={`/settings/${s.id}`} className={({ isActive }) => (isActive ? 'mi on' : 'mi')}>
            <Icon name={s.icon} />
            {s.label}
          </NavLink>
        ))}
        {/* Administrators also reach the panel from here, not only from the account menu */}
        {user.isAdmin && (
          <NavLink to="/admin/users" className="mi">
            <Icon name="cog" />
            Administration
          </NavLink>
        )}
      </nav>
      <div className="set-body">
        <h1>{current.label}</h1>
        <Body />
      </div>
    </div>
  );
}
