// /settings/:section (PLAN §7.3, task 8.9): a section list on the left, the section on the right.

import { Navigate, NavLink, useParams } from 'react-router';

import { useMeta } from '../api/hooks/me';
import { Icon } from '../ui/Icon';
import { RulesSettings } from '../rules/RulesSettings';
import { ActivitySettings } from './Activity';
import { AppearanceControls } from './Appearance';
import { CategoriesSettings } from './Categories';
import { DataSettings } from './Data';
import { FriendsSettings } from './Friends';
import { ProfileSettings } from './Profile';
import { SECTIONS, type SectionId } from './sections';
import { SessionsSettings } from './Sessions';

const BODY: Record<SectionId, () => React.JSX.Element> = {
  appearance: () => (
    <div className="set-card">
      <div className="lbl">Appearance</div>
      <AppearanceControls />
    </div>
  ),
  profile: ProfileSettings,
  sessions: SessionsSettings,
  activity: ActivitySettings,
  friends: FriendsSettings,
  categories: CategoriesSettings,
  rules: RulesSettings,
  data: DataSettings,
};

export function SettingsPage() {
  const { section } = useParams();
  const meta = useMeta().data;
  // Friends only where the instance has them: switched on, and not a demo (ADR 0027)
  const sections = SECTIONS.filter((s) => s.id !== 'friends' || (meta?.features.friends !== false && !meta?.demoMode));
  const current = sections.find((s) => s.id === section);
  if (!current) return <Navigate to="/settings/appearance" replace />;
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
        <NavLink to="/" className="mi">
          <Icon name="left" />
          Back to transactions
        </NavLink>
      </nav>
      <div className="set-body">
        <h1>{current.label}</h1>
        <Body />
      </div>
    </div>
  );
}
