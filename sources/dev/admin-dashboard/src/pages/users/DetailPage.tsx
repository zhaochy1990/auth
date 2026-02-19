import { useTranslation } from 'react-i18next';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useParams, useNavigate } from 'react-router';
import { ArrowLeft, Check, Pencil, Shield, ShieldOff, Unlink, X } from 'lucide-react';
import { getUser, getUserAccounts, updateUser, adminUnlinkAccount } from '../../api/admin';
import StatusBadge from '../../components/shared/StatusBadge';
import Badge from '../../components/ui/Badge';
import ConfirmDialog from '../../components/ui/ConfirmDialog';
import Spinner from '../../components/ui/Spinner';
import toast from 'react-hot-toast';
import { useState } from 'react';

export default function UserDetailPage() {
  const { t } = useTranslation('users');
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const [unlinkProvider, setUnlinkProvider] = useState<string | null>(null);
  const [editingName, setEditingName] = useState(false);
  const [nameInput, setNameInput] = useState('');

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

  const unlinkMutation = useMutation({
    mutationFn: (providerId: string) => adminUnlinkAccount(id!, providerId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['userAccounts', id] });
      setUnlinkProvider(null);
      toast.success(t('detail.unlinkSuccess'));
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

  return (
    <div className="mx-auto max-w-2xl">
      <button
        onClick={() => navigate('/users')}
        className="flex items-center gap-1 text-sm text-gray-500 hover:text-gray-700"
      >
        <ArrowLeft size={16} />
        {t('common:actions.back')}
      </button>

      <h1 className="mt-4 text-2xl font-semibold text-gray-900">
        {user.email || user.name || user.id}
      </h1>

      {/* Profile */}
      <div className="mt-6 rounded-lg bg-white p-6 shadow-sm ring-1 ring-gray-200">
        <h2 className="font-medium text-gray-900">{t('detail.profile')}</h2>

        <dl className="mt-4 grid grid-cols-2 gap-4">
          <div>
            <dt className="text-xs font-medium text-gray-500">{t('detail.email')}</dt>
            <dd className="mt-1 text-sm text-gray-900">{user.email || '-'}</dd>
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
                  <span className="text-sm text-gray-900">{user.name || '-'}</span>
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

        <div className="mt-6 flex gap-2">
          <button
            onClick={() => roleMutation.mutate(user.role === 'admin' ? 'user' : 'admin')}
            disabled={roleMutation.isPending}
            className="flex items-center gap-1 rounded-md bg-gray-100 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-200 disabled:opacity-50"
          >
            {user.role === 'admin' ? <ShieldOff size={16} /> : <Shield size={16} />}
            {t('detail.changeRole')} → {user.role === 'admin' ? t('role.user') : t('role.admin')}
          </button>
          <button
            onClick={() => activeMutation.mutate(!user.is_active)}
            disabled={activeMutation.isPending}
            className="rounded-md bg-gray-100 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-200 disabled:opacity-50"
          >
            {t('detail.toggleActive')} → {user.is_active ? t('common:actions.disable') : t('common:actions.enable')}
          </button>
        </div>
      </div>

      {/* Accounts */}
      <div className="mt-6 rounded-lg bg-white p-6 shadow-sm ring-1 ring-gray-200">
        <h2 className="font-medium text-gray-900">{t('detail.accounts')}</h2>

        {(accounts || []).length === 0 ? (
          <p className="mt-4 text-sm text-gray-500">{t('common:status.empty')}</p>
        ) : (
          <div className="mt-4 divide-y divide-gray-100">
            {(accounts || []).map((a) => (
              <div key={a.id} className="flex items-center justify-between py-3">
                <div>
                  <div className="text-sm font-medium">{a.provider_id}</div>
                  <div className="text-xs text-gray-500">
                    {t('detail.accountId')}: {a.provider_account_id || '-'}
                  </div>
                  <div className="text-xs text-gray-400">
                    {new Date(a.created_at).toLocaleString()}
                  </div>
                </div>
                <button
                  onClick={() => setUnlinkProvider(a.provider_id)}
                  className="flex items-center gap-1 text-sm text-red-600 hover:text-red-800"
                >
                  <Unlink size={14} />
                  {t('detail.unlinkAccount')}
                </button>
              </div>
            ))}
          </div>
        )}
      </div>

      <ConfirmDialog
        open={!!unlinkProvider}
        message={t('detail.unlinkConfirm')}
        onConfirm={() => unlinkProvider && unlinkMutation.mutate(unlinkProvider)}
        onCancel={() => setUnlinkProvider(null)}
        loading={unlinkMutation.isPending}
      />
    </div>
  );
}
