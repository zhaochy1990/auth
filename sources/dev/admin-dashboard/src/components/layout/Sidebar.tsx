import { useTranslation } from 'react-i18next';
import { NavLink } from 'react-router';
import { LayoutDashboard, AppWindow, Users } from 'lucide-react';

const links = [
  { to: '/', icon: LayoutDashboard, labelKey: 'sidebar.dashboard' },
  { to: '/applications', icon: AppWindow, labelKey: 'sidebar.applications' },
  { to: '/users', icon: Users, labelKey: 'sidebar.users' },
];

export default function Sidebar() {
  const { t } = useTranslation();

  return (
    <aside className="flex h-screen w-56 flex-col border-r border-gray-200 bg-white">
      <div className="flex h-14 items-center px-4 font-semibold text-gray-900">
        {t('appName')}
      </div>
      <nav className="flex-1 space-y-1 px-2 py-2">
        {links.map(({ to, icon: Icon, labelKey }) => (
          <NavLink
            key={to}
            to={to}
            end={to === '/'}
            className={({ isActive }) =>
              `flex items-center gap-2 rounded-md px-3 py-2 text-sm ${
                isActive ? 'bg-blue-50 font-medium text-blue-700' : 'text-gray-700 hover:bg-gray-100'
              }`
            }
          >
            <Icon size={18} />
            {t(labelKey)}
          </NavLink>
        ))}
      </nav>
    </aside>
  );
}
