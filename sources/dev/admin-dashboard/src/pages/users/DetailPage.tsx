import { useTranslation } from 'react-i18next';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useParams, useNavigate } from 'react-router';
import { ArrowLeft, Check, Copy, KeyRound, Pencil, Shield, ShieldOff, Trash2, Unlink, X } from 'lucide-react';
import { getUser, getUserAccounts, updateUser, adminUnlinkAccount, deleteUser, resetUserPassword } from '../../api/admin';
import StatusBadge from '../../components/shared/StatusBadge';
import Badge from '../../components/ui/Badge';
import ConfirmDialog from '../../components/ui/ConfirmDialog';
import ResetPasswordDialog from '../../components/ui/ResetPasswordDialog';
import Spinner from '../../components/ui/Spinner';
import toast from 'react-hot-toast';
import { useState } from 'react';
import { isAxiosError } from 'axios';
import { useAuthStore } from '../../store/authStore';

export default function UserDetailPage() {
  const { t } = useTranslation('users');
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const currentUserId = useAuthStore((s) => s.userId);
  const logout = useAuthStore((s) => s.logout);

  const [unlinkProvider, setUnlinkProvider] = useState<string | null>(null);
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false);
  const [editingName, setEditingName] = useState(false);
  const [nameInput, setNameInput] = useState('');
  const [editingNote, setEditingNote] = useState(false);
  const [noteInput, setNoteInput] = useState('');
  const [resetPasswordOpen, setResetPasswordOpen] = useState(false);
  const [resetPasswordError, setResetPasswordError] = useState('');
  const [copiedUserId, setCopiedUserId] = useState(false);

  const { data: user, isLoading } = useQuery({
    queryKey: ['user', id],
    queryFn: () => getUser(id!),
    enabled: !!id,
  });

  const { data: accounts } = useQuery({
    queryKey: ['userAccounts', id],
    queryFn: () => getUserAccounts(id!),
    enabled: !!id,
  });

  const roleMutation = useMutation({
    mutationFn: (role: string) => updateUser(id!, { role }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['user', id] });
      queryClient.invalidateQueries({ queryKey: ['users'] });
      toast.success(t('detail.updateSuccess'));
    },
  });

  const activeMutation = useMutation({
    mutationFn: (is_active: boolean) => updateUser(id!, { is_active }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['user', id] });
      queryClient.invalidateQueries({ queryKey: ['users'] });
      toast.success(t('detail.updateSuccess'));
    },
  });

  const nameMutation = useMutation({
    mutationFn: (name: string) => updateUser(id!, { name }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['user', id] });
      queryClient.invalidateQueries({ queryKey: ['users'] });
      setEditingName(false);
      toast.success(t('detail.updateSuccess'));
    },
  });

  const noteMutation = useMutation({
    mutationFn: (note: string) => updateUser(id!, { note }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['user', id] });
      setEditingNote(false);
      toast.success(t('detail.updateSuccess'));
    },
  });

  const unlinkMutation = useMutation({
    mutationFn: (providerId: string) => adminUnlinkAccount(id!, providerId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['userAccounts', id] });
      setUnlinkProvider(null);
      toast.success(t('detail.unlinkSuccess'));
    },
  });

  const deleteMutation = useMutation({
    mutationFn: () => deleteUser(id!),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'] });
      setDeleteConfirmOpen(false);
      toast.success(t('detail.deleteSuccess'));

      if (id === currentUserId) {
        logout();
        navigate('/login', { replace: true });
      } else {
        navigate('/users', { replace: true });
      }
    },
  });

  const resetPasswordMutation = useMutation({
    mutationFn: (data: { password: string; revoke_sessions: boolean }) =>
      resetUserPassword(id!, data),
    onSuccess: (_, vars) => {
      toast.success(t('detail.password.success'));
      setResetPasswordOpen(false);
      setResetPasswordError('');

      if (id === currentUserId && vars.revoke_sessions) {
        logout();
        navigate('/login', { replace: true });
      }
    },
    onError: (err) => {
      if (isAxiosError(err) && err.response?.data?.message) {
        setResetPasswordError(err.response.data.message);
      } else {
        setResetPasswordError(String(err));
      }
    },
  });

  if (isLoading) {
    return (
      <div className="flex h-64 items-center justify-center">
        <Spinner className="h-6 w-6 text-gray-400" />
      </div>
    );
  }

  if (!user) {
    return <div className="text-gray-500">User not found</div>;
  }

  const copyUserId = async () => {
    await navigator.clipboard.writeText(user.id);
    setCopiedUserId(true);
    setTimeout(() => setCopiedUserId(false), 2000);
  };

  return (
    <div className="mx-auto w-full max-w-2xl">
      <button
        onClick={() => navigate('/users')}
        className="flex items-center gap-1 text-sm text-gray-500 hover:text-gray-700"
      >
        <ArrowLeft size={16} />
        {t('common:actions.back')}
      </button>

      <h1 className="mt-4 break-words text-xl font-semibold text-gray-900 sm:text-2xl">
        {user.email || user.name || user.id}
      </h1>

      {/* Profile */}
      <div className="mt-6 rounded-lg bg-white p-4 shadow-sm ring-1 ring-gray-200 sm:p-6">
        <h2 className="font-medium text-gray-900">{t('detail.profile')}</h2>

        <dl className="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div className="sm:col-span-2">
            <dt className="text-xs font-medium text-gray-500">{t('detail.userId')}</dt>
            <dd className="mt-1 flex items-center gap-1">
              <span className="break-all font-mono text-sm text-gray-900">{user.id}</span>
              <button
                onClick={copyUserId}
                aria-label={t('common:actions.copy')}
                title={copiedUserId ? t('common:actions.copied') : t('common:actions.copy')}
                className="shrink-0 text-gray-400 hover:text-gray-600"
              >
                {copiedUserId ? <Check size={14} className="text-green-600" /> : <Copy size={14} />}
              </button>
            </dd>
          </div>
          <div>
            <dt className="text-xs font-medium text-gray-500">{t('detail.email')}</dt>
            <dd className="mt-1 break-all text-sm text-gray-900">{user.email || '-'}</dd>
          </div>
          <div>
            <dt className="text-xs font-medium text-gray-500">{t('detail.name')}</dt>
            <dd className="mt-1">
              {editingName ? (
                <div className="flex items-center gap-1">
                  <input
                    type="text"
                    value={nameInput}
                    onChange={(e) => setNameInput(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') nameMutation.mutate(nameInput);
                      if (e.key === 'Escape') setEditingName(false);
                    }}
                    placeholder={t('detail.namePlaceholder')}
                    className="w-full rounded-md border border-gray-300 px-2 py-1 text-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
                    autoFocus
                  />
                  <button
                    onClick={() => nameMutation.mutate(nameInput)}
                    disabled={nameMutation.isPending}
                    className="text-green-600 hover:text-green-800 disabled:opacity-50"
                  >
                    <Check size={16} />
                  </button>
                  <button
                    onClick={() => setEditingName(false)}
                    className="text-gray-400 hover:text-gray-600"
                  >
                    <X size={16} />
                  </button>
                </div>
              ) : (
                <div className="flex items-center gap-1">
                  <span className="break-words text-sm text-gray-900">{user.name || '-'}</span>
                  <button
                    onClick={() => { setNameInput(user.name || ''); setEditingName(true); }}
                    className="text-gray-400 hover:text-gray-600"
                    title={t('detail.editName')}
                  >
                    <Pencil size={14} />
                  </button>
                </div>
              )}
            </dd>
          </div>
          <div>
            <dt className="text-xs font-medium text-gray-500">{t('detail.role')}</dt>
            <dd className="mt-1">
              <Badge variant={user.role === 'admin' ? 'yellow' : 'gray'}>
                {t(`role.${user.role}`)}
              </Badge>
            </dd>
          </div>
          <div>
            <dt className="text-xs font-medium text-gray-500">{t('detail.status')}</dt>
            <dd className="mt-1">
              <StatusBadge active={user.is_active} />
            </dd>
          </div>
          <div>
            <dt className="text-xs font-medium text-gray-500">
              {user.email_verified ? t('detail.emailVerified') : t('detail.emailNotVerified')}
            </dt>
          </div>
          <div>
            <dt className="text-xs font-medium text-gray-500">{t('detail.createdAt')}</dt>
            <dd className="mt-1 text-sm text-gray-900">{new Date(user.created_at).toLocaleString()}</dd>
          </div>
        </dl>

        <div className="mt-6 flex flex-col gap-2 sm:flex-row">
          <button
            onClick={() => roleMutation.mutate(user.role === 'admin' ? 'user' : 'admin')}
            disabled={roleMutation.isPending}
            className="flex w-full items-center justify-center gap-1 rounded-md bg-gray-100 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-200 disabled:opacity-50 sm:w-auto"
          >
            {user.role === 'admin' ? <ShieldOff size={16} /> : <Shield size={16} />}
            {t('detail.changeRole')} → {user.role === 'admin' ? t('role.user') : t('role.admin')}
          </button>
          <button
            onClick={() => activeMutation.mutate(!user.is_active)}
            disabled={activeMutation.isPending}
            className="w-full rounded-md bg-gray-100 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-200 disabled:opacity-50 sm:w-auto"
          >
            {t('detail.toggleActive')} → {user.is_active ? t('common:actions.disable') : t('common:actions.enable')}
          </button>
        </div>
      </div>

      {/* Admin Note */}
      <div className="mt-6 rounded-lg bg-white p-4 shadow-sm ring-1 ring-gray-200 sm:p-6">
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <h2 className="font-medium text-gray-900">{t('detail.note')}</h2>
          {!editingNote && (
            <button
              onClick={() => { setNoteInput(user.note || ''); setEditingNote(true); }}
              className="flex items-center gap-1 self-start text-sm text-gray-500 hover:text-gray-700"
              title={t('detail.noteEdit')}
            >
              <Pencil size={14} />
              {t('detail.noteEdit')}
            </button>
          )}
        </div>

        {editingNote ? (
          <div className="mt-3">
            <textarea
              value={noteInput}
              onChange={(e) => setNoteInput(e.target.value)}
              placeholder={t('detail.notePlaceholder')}
              rows={4}
              className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
              autoFocus
            />
            <div className="mt-2 flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
              <button
                onClick={() => setEditingNote(false)}
                className="rounded-md bg-gray-100 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-200"
              >
                {t('detail.noteCancel')}
              </button>
              <button
                onClick={() => noteMutation.mutate(noteInput)}
                disabled={noteMutation.isPending}
                className="rounded-md bg-blue-600 px-3 py-1.5 text-sm text-white hover:bg-blue-700 disabled:opacity-50"
              >
                {t('detail.noteSave')}
              </button>
            </div>
          </div>
        ) : (
          <div className="mt-3 whitespace-pre-wrap text-sm">
            {user.note ? (
              <span className="text-gray-900">{user.note}</span>
            ) : (
              <span className="text-gray-400">{t('detail.noteEmpty')}</span>
            )}
          </div>
        )}
      </div>

      {/* Recent Logins */}
      <div className="mt-6 rounded-lg bg-white p-4 shadow-sm ring-1 ring-gray-200 sm:p-6">
        <h2 className="font-medium text-gray-900">{t('detail.recentLogins')}</h2>
        {user.recent_logins && user.recent_logins.length > 0 ? (
          <ul className="mt-3 divide-y divide-gray-100">
            {user.recent_logins.map((login, idx) => (
              <li key={idx} className="flex flex-col gap-1 py-2 sm:flex-row sm:items-center sm:justify-between">
                <span className="text-sm text-gray-900">
                  {new Date(login.at).toLocaleString()}
                </span>
                <span className="break-all text-xs text-gray-500">
                  {t('detail.loginIp')}: {login.ip}
                </span>
              </li>
            ))}
          </ul>
        ) : (
          <p className="mt-3 text-sm text-gray-500">{t('detail.noLogins')}</p>
        )}
      </div>

      {/* Accounts */}
      <div className="mt-6 rounded-lg bg-white p-4 shadow-sm ring-1 ring-gray-200 sm:p-6">
        <h2 className="font-medium text-gray-900">{t('detail.accounts')}</h2>

        {(accounts || []).length === 0 ? (
          <p className="mt-4 text-sm text-gray-500">{t('common:status.empty')}</p>
        ) : (
          <div className="mt-4 divide-y divide-gray-100">
            {(accounts || []).map((a) => (
              <div key={a.id} className="flex flex-col gap-3 py-3 sm:flex-row sm:items-center sm:justify-between">
                <div className="min-w-0">
                  <div className="text-sm font-medium">{a.provider_id}</div>
                  <div className="break-all text-xs text-gray-500">
                    {t('detail.accountId')}: {a.provider_account_id || '-'}
                  </div>
                  <div className="text-xs text-gray-400">
                    {new Date(a.created_at).toLocaleString()}
                  </div>
                </div>
                <button
                  onClick={() => setUnlinkProvider(a.provider_id)}
                  className="flex items-center gap-1 self-start text-sm text-red-600 hover:text-red-800"
                >
                  <Unlink size={14} />
                  {t('detail.unlinkAccount')}
                </button>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Password */}
      {(accounts || []).some((a) => a.provider_id === 'password') && (
        <div className="mt-6 rounded-lg bg-white p-4 shadow-sm ring-1 ring-gray-200 sm:p-6">
          <h2 className="font-medium text-gray-900">{t('detail.password.title')}</h2>
          <p className="mt-2 text-sm text-gray-600">{t('detail.password.description')}</p>
          <button
            onClick={() => { setResetPasswordError(''); setResetPasswordOpen(true); }}
            className="mt-4 flex w-full items-center justify-center gap-1 rounded-md bg-gray-100 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-200 sm:w-auto"
          >
            <KeyRound size={16} />
            {t('detail.password.resetBtn')}
          </button>
        </div>
      )}

      {/* Danger Zone */}
      <div className="mt-6 rounded-lg bg-white p-4 shadow-sm ring-1 ring-red-200 sm:p-6">
        <h2 className="font-medium text-red-700">{t('detail.dangerZone')}</h2>
        <p className="mt-2 text-sm text-gray-600">{t('detail.deleteDescription')}</p>
        <button
          onClick={() => setDeleteConfirmOpen(true)}
          disabled={deleteMutation.isPending}
          className="mt-4 flex w-full items-center justify-center gap-1 rounded-md bg-red-600 px-3 py-1.5 text-sm text-white hover:bg-red-700 disabled:opacity-50 sm:w-auto"
        >
          <Trash2 size={16} />
          {t('detail.deleteUser')}
        </button>
      </div>

      <ConfirmDialog
        open={!!unlinkProvider}
        message={t('detail.unlinkConfirm')}
        onConfirm={() => unlinkProvider && unlinkMutation.mutate(unlinkProvider)}
        onCancel={() => setUnlinkProvider(null)}
        loading={unlinkMutation.isPending}
      />
      <ConfirmDialog
        open={deleteConfirmOpen}
        title={t('detail.deleteConfirmTitle')}
        message={t('detail.deleteConfirm')}
        onConfirm={() => deleteMutation.mutate()}
        onCancel={() => setDeleteConfirmOpen(false)}
        loading={deleteMutation.isPending}
      />
      {resetPasswordOpen && (
        <ResetPasswordDialog
          open={resetPasswordOpen}
          isSelf={id === currentUserId}
          loading={resetPasswordMutation.isPending}
          errorMessage={resetPasswordError}
          onSubmit={(data) => resetPasswordMutation.mutate(data)}
          onCancel={() => { setResetPasswordOpen(false); setResetPasswordError(''); }}
        />
      )}
    </div>
  );
}
