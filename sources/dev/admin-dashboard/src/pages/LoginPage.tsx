import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate, Navigate } from 'react-router';
import { Globe } from 'lucide-react';
import { useAuthStore } from '../store/authStore';
import Spinner from '../components/ui/Spinner';

export default function LoginPage() {
  const { t, i18n } = useTranslation('login');
  const navigate = useNavigate();
  const { login, isAuthenticated } = useAuthStore();

  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  if (isAuthenticated) return <Navigate to="/" replace />;

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setError('');
    setLoading(true);
    try {
      await login(email, password);
      navigate('/', { replace: true });
    } catch (err: any) {
      if (err.message === 'INSUFFICIENT_PERMISSIONS') {
        setError(t('error.insufficientPermissions'));
      } else if (err.response?.data?.error === 'user_disabled') {
        setError(t('error.userDisabled'));
      } else if (err.response?.status === 401) {
        setError(t('error.invalidCredentials'));
      } else {
        setError(t('error.generic'));
      }
    } finally {
      setLoading(false);
    }
  };

  const toggleLang = () => {
    const next = i18n.language === 'zh-CN' ? 'en-US' : 'zh-CN';
    i18n.changeLanguage(next);
    localStorage.setItem('lang', next);
  };

  return (
    <div className="flex min-h-screen items-center justify-center bg-gray-50 px-4">
      <div className="w-full max-w-sm">
        <div className="flex justify-end mb-4">
          <button onClick={toggleLang} className="flex items-center gap-1 text-sm text-gray-500 hover:text-gray-700">
            <Globe size={16} />
            {i18n.language === 'zh-CN' ? 'EN' : '中文'}
          </button>
        </div>

        <div className="rounded-lg bg-white p-8 shadow-sm ring-1 ring-gray-200">
          <h1 className="text-center text-xl font-semibold text-gray-900">{t('title')}</h1>

          {error && (
            <div className="mt-4 rounded-md bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>
          )}

          <form onSubmit={handleSubmit} className="mt-6 space-y-4">
            <div>
              <label className="block text-sm font-medium text-gray-700">{t('email')}</label>
              <input
                type="email"
                required
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700">{t('password')}</label>
              <input
                type="password"
                required
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
              />
            </div>
            <button
              type="submit"
              disabled={loading}
              className="flex w-full items-center justify-center gap-2 rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50"
            >
              {loading && <Spinner className="h-4 w-4" />}
              {loading ? t('loggingIn') : t('submit')}
            </button>
          </form>
        </div>
      </div>
    </div>
  );
}
