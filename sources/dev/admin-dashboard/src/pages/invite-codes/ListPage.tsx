import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Plus, Copy, Check, Ticket } from 'lucide-react';
import { listInviteCodes, createInviteCode, revokeInviteCode } from '../../api/admin';
import type { InviteCode } from '../../api/types';
import Spinner from '../../components/ui/Spinner';

export default function InviteCodeListPage() {
  const { t } = useTranslation('inviteCodes');
  const { t: tc } = useTranslation();
  const queryClient = useQueryClient();

  const [newCode, setNewCode] = useState<InviteCode | null>(null);
  const [copiedCode, setCopiedCode] = useState(false);

  const { data, isLoading } = useQuery({
    queryKey: ['invite-codes'],
    queryFn: listInviteCodes,
  });

  const createMutation = useMutation({
    mutationFn: createInviteCode,
    onSuccess: (code) => {
      setNewCode(code);
      queryClient.invalidateQueries({ queryKey: ['invite-codes'] });
    },
  });

  const revokeMutation = useMutation({
    mutationFn: revokeInviteCode,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['invite-codes'] });
    },
  });

  const handleCopy = async (code: string) => {
    await navigator.clipboard.writeText(code);
    setCopiedCode(true);
    setTimeout(() => setCopiedCode(false), 2000);
  };

  const closeModal = () => setNewCode(null);

  if (isLoading) {
    return (
      <div className="flex h-64 items-center justify-center">
        <Spinner className="h-6 w-6 text-gray-400" />
      </div>
    );
  }

  return (
    <div>
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold text-gray-900">{t('title')}</h1>
        <button
          onClick={() => createMutation.mutate()}
          disabled={createMutation.isPending}
          className="flex items-center gap-1 rounded-md bg-blue-600 px-3 py-2 text-sm text-white hover:bg-blue-700 disabled:opacity-50"
        >
          <Plus size={16} />
          {t('createBtn')}
        </button>
      </div>

      <div className="mt-4 overflow-hidden rounded-lg bg-white shadow-sm ring-1 ring-gray-200">
        <table className="min-w-full divide-y divide-gray-200">
          <thead className="bg-gray-50">
            <tr>
              <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">{t('table.code')}</th>
              <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">{t('table.createdAt')}</th>
              <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">{t('table.used')}</th>
              <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">{t('table.usedBy')}</th>
              <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">{t('table.actions')}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-200">
            {(data || []).length === 0 ? (
              <tr>
                <td colSpan={5} className="px-4 py-8 text-center text-sm text-gray-500">
                  {tc('status.empty')}
                </td>
              </tr>
            ) : (
              (data || []).map((item) => (
                <tr key={item.id} className="hover:bg-gray-50">
                  <td className="px-4 py-3">
                    <code className="rounded bg-gray-100 px-1.5 py-0.5 text-xs font-mono text-gray-700">
                      {item.code}
                    </code>
                  </td>
                  <td className="px-4 py-3 text-sm text-gray-500">
                    {new Date(item.created_at).toLocaleDateString()}
                  </td>
                  <td className="px-4 py-3 text-sm">
                    {item.used_at ? (
                      <span className="text-green-600">{t('status.used')}</span>
                    ) : item.is_revoked ? (
                      <span className="text-red-500">{t('status.revoked')}</span>
                    ) : (
                      <span className="text-gray-400">{t('status.unused')}</span>
                    )}
                  </td>
                  <td className="px-4 py-3 text-sm text-gray-500">
                    {item.used_by || '-'}
                  </td>
                  <td className="px-4 py-3">
                    {!item.used_at && !item.is_revoked && (
                      <button
                        onClick={() => revokeMutation.mutate(item.code)}
                        disabled={revokeMutation.isPending}
                        className="text-sm text-red-600 hover:text-red-800 disabled:opacity-50"
                      >
                        {t('actions.revoke')}
                      </button>
                    )}
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      {/* New code modal */}
      {newCode && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="w-full max-w-sm rounded-lg bg-white p-6 shadow-xl">
            <div className="mb-4 flex items-center gap-2">
              <Ticket size={20} className="text-blue-600" />
              <h2 className="text-lg font-semibold text-gray-900">{t('modal.title')}</h2>
            </div>
            <p className="mb-3 text-sm text-gray-500">{t('modal.description')}</p>
            <div className="flex items-center gap-2 rounded-md border border-gray-200 bg-gray-50 px-3 py-2">
              <code className="flex-1 font-mono text-sm text-gray-800">{newCode.code}</code>
              <button
                onClick={() => handleCopy(newCode.code)}
                className="text-gray-400 hover:text-gray-600"
                title={tc('actions.copy')}
              >
                {copiedCode ? (
                  <Check size={16} className="text-green-600" />
                ) : (
                  <Copy size={16} />
                )}
              </button>
            </div>
            <p className="mt-2 text-xs text-gray-400">{t('modal.warning')}</p>
            <div className="mt-4 flex justify-end">
              <button
                onClick={closeModal}
                className="rounded-md bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700"
              >
                {tc('actions.confirm')}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
