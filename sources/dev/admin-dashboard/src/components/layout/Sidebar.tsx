import { useTranslation } from 'react-i18next';
import { NavLink } from 'react-router';
import { LayoutDashboard, AppWindow, Users, Ticket, UsersRound, X } from 'lucide-react';

const links = [
  { to: '/', icon: LayoutDashboard, labelKey: 'sidebar.dashboard' },
  { to: '/applications', icon: AppWindow, labelKey: 'sidebar.applications' },
  { to: '/users', icon: Users, labelKey: 'sidebar.users' },
  { to: '/teams', icon: UsersRound, labelKey: 'sidebar.teams' },
  { to: '/invite-codes', icon: Ticket, labelKey: 'sidebar.inviteCodes' },
];

interface Props {
  open: boolean;
  onClose: () => void;
}

function SidebarNav({ onNavigate }: { onNavigate?: () => void }) {
  const { t } = useTranslation();

  return (
    <nav className="flex-1 space-y-1 px-2 py-2">
      {links.map(({ to, icon: Icon, labelKey }) => (
        <NavLink
          key={to}
          to={to}
          end={to === '/'}
          onClick={onNavigate}
          className={({ isActive }) =>
            `flex items-center gap-2 rounded-md px-3 py-2 text-sm ${
              isActive ? 'bg-blue-50 font-medium text-blue-700' : 'text-gray-700 hover:bg-gray-100'
            }`
          }
        >
          <Icon size={18} className="shrink-0" />
          <span className="truncate">{t(labelKey)}</span>
        </NavLink>
      ))}
    </nav>
  );
}

export default function Sidebar({ open, onClose }: Props) {
  const { t } = useTranslation();

  return (
    <>
      <aside className="hidden min-h-dvh w-56 shrink-0 flex-col border-r border-gray-200 bg-white md:flex">
        <div className="flex h-14 items-center px-4 font-semibold text-gray-900">
          {t('appName')}
        </div>
        <SidebarNav />
      </aside>

      {open && (
        <div className="fixed inset-0 z-40 md:hidden">
          <button
            type="button"
            aria-label={t('actions.closeMenu')}
            onClick={onClose}
            className="absolute inset-0 h-full w-full bg-black/40"
          />
          <aside className="relative z-10 flex h-dvh w-64 max-w-[85vw] flex-col border-r border-gray-200 bg-white shadow-xl">
            <div className="flex h-14 items-center justify-between gap-3 px-4 font-semibold text-gray-900">
              <span className="truncate">{t('appName')}</span>
              <button
                type="button"
                onClick={onClose}
                aria-label={t('actions.closeMenu')}
                className="rounded-md p-2 text-gray-500 hover:bg-gray-100"
              >
                <X size={18} />
              </button>
            </div>
            <SidebarNav onNavigate={onClose} />
          </aside>
        </div>
      )}
    </>
  );
}
