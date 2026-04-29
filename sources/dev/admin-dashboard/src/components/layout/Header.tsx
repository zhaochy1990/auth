import { useTranslation } from 'react-i18next';
import { LogOut, Globe, Menu } from 'lucide-react';
import { useAuthStore } from '../../store/authStore';

interface Props {
  onMenuClick: () => void;
}

export default function Header({ onMenuClick }: Props) {
  const { t, i18n } = useTranslation();
  const logout = useAuthStore((s) => s.logout);

  const toggleLang = () => {
    const next = i18n.language === 'zh-CN' ? 'en-US' : 'zh-CN';
    i18n.changeLanguage(next);
    localStorage.setItem('lang', next);
  };

  return (
    <header className="sticky top-0 z-30 flex min-h-14 items-center justify-between gap-3 border-b border-gray-200 bg-white px-4">
      <div className="flex min-w-0 items-center gap-2 md:hidden">
        <button
          type="button"
          onClick={onMenuClick}
          aria-label={t('actions.openMenu')}
          className="rounded-md p-2 text-gray-600 hover:bg-gray-100"
        >
          <Menu size={20} />
        </button>
        <span className="truncate text-sm font-semibold text-gray-900">{t('appName')}</span>
      </div>

      <div className="ml-auto flex items-center gap-2">
        <button
          onClick={toggleLang}
          className="flex shrink-0 items-center gap-1 rounded-md px-2 py-1 text-sm text-gray-600 hover:bg-gray-100"
        >
          <Globe size={16} />
          {i18n.language === 'zh-CN' ? 'EN' : '中文'}
        </button>
        <button
          onClick={() => {
            logout();
            window.location.href = '/login';
          }}
          className="flex shrink-0 items-center gap-1 rounded-md px-2 py-1 text-sm text-gray-600 hover:bg-gray-100"
        >
          <LogOut size={16} />
          {t('actions.logout')}
        </button>
      </div>
    </header>
  );
}
