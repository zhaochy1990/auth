import { useTranslation } from 'react-i18next';
import { LogOut, Globe } from 'lucide-react';
import { useAuthStore } from '../../store/authStore';

export default function Header() {
  const { t, i18n } = useTranslation();
  const logout = useAuthStore((s) => s.logout);

  const toggleLang = () => {
    const next = i18n.language === 'zh-CN' ? 'en-US' : 'zh-CN';
    i18n.changeLanguage(next);
    localStorage.setItem('lang', next);
  };

  return (
    <header className="flex h-14 items-center justify-end gap-3 border-b border-gray-200 bg-white px-4">
      <button
        onClick={toggleLang}
        className="flex items-center gap-1 rounded-md px-2 py-1 text-sm text-gray-600 hover:bg-gray-100"
      >
        <Globe size={16} />
        {i18n.language === 'zh-CN' ? 'EN' : '中文'}
      </button>
      <button
        onClick={() => {
          logout();
          window.location.href = '/login';
        }}
        className="flex items-center gap-1 rounded-md px-2 py-1 text-sm text-gray-600 hover:bg-gray-100"
      >
        <LogOut size={16} />
        {t('actions.logout')}
      </button>
    </header>
  );
}
