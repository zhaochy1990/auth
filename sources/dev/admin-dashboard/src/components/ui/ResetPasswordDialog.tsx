import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { Eye, EyeOff } from 'lucide-react';

interface Props {
  open: boolean;
  isSelf: boolean;
  loading?: boolean;
  errorMessage?: string;
  onSubmit: (data: { password: string; revoke_sessions: boolean }) => void;
  onCancel: () => void;
}

export default function ResetPasswordDialog({
  open,
  isSelf,
  loading,
  errorMessage,
  onSubmit,
  onCancel,
}: Props) {
  const { t } = useTranslation('users');
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [revokeSessions, setRevokeSessions] = useState(true);

  if (!open) return null;

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    onSubmit({ password, revoke_sessions: revokeSessions });
  };

  const handleCancel = () => {
    setPassword('');
    setShowPassword(false);
    setRevokeSessions(true);
    onCancel();
  };

  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center overflow-y-auto bg-black/40 p-4 sm:items-center">
      <div className="max-h-[calc(100dvh-2rem)] w-full max-w-md overflow-y-auto rounded-lg bg-white p-4 shadow-xl sm:p-6">
        <h3 className="text-lg font-semibold text-gray-900">
          {t('detail.password.dialogTitle')}
        </h3>

        <form onSubmit={handleSubmit} className="mt-4 space-y-4">
          {errorMessage && (
            <div className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700">
              {errorMessage}
            </div>
          )}

          <div>
            <label className="block text-sm font-medium text-gray-700">
              {t('detail.password.newPassword')}
            </label>
            <div className="mt-1 flex items-center gap-1">
              <input
                type={showPassword ? 'text' : 'password'}
                required
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder={t('detail.password.newPasswordPlaceholder')}
                className="block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
                autoFocus
              />
              <button
                type="button"
                onClick={() => setShowPassword((v) => !v)}
                className="rounded-md px-2 py-2 text-gray-500 hover:bg-gray-100"
                title={
                  showPassword
                    ? t('detail.password.hidePassword')
                    : t('detail.password.showPassword')
                }
              >
                {showPassword ? <EyeOff size={16} /> : <Eye size={16} />}
              </button>
            </div>
            <p className="mt-1 text-xs text-gray-500">
              {t('detail.password.passwordHint')}
            </p>
          </div>

          <label className="flex items-start gap-2 text-sm text-gray-700">
            <input
              type="checkbox"
              checked={revokeSessions}
              onChange={(e) => setRevokeSessions(e.target.checked)}
              className="mt-0.5 h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500"
            />
            <span>{t('detail.password.revokeSessions')}</span>
          </label>

          {isSelf && revokeSessions && (
            <div className="rounded-md bg-amber-50 px-3 py-2 text-sm text-amber-800">
              {t('detail.password.selfWarning')}
            </div>
          )}

          <div className="flex flex-col-reverse gap-2 pt-2 sm:flex-row sm:justify-end">
            <button
              type="button"
              onClick={handleCancel}
              disabled={loading}
              className="rounded-md px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-100"
            >
              {t('detail.password.cancel')}
            </button>
            <button
              type="submit"
              disabled={loading || password.length === 0}
              className="rounded-md bg-blue-600 px-3 py-1.5 text-sm text-white hover:bg-blue-700 disabled:opacity-50"
            >
              {loading ? '...' : t('detail.password.submit')}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
