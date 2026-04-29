import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate, NavLink } from 'react-router';
import { useMutation } from '@tanstack/react-query';
import { AxiosError } from 'axios';
import toast from 'react-hot-toast';
import { adminCreateTeam } from '../../api/admin';

export default function TeamCreatePage() {
  const { t } = useTranslation('teams');
  const navigate = useNavigate();

  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [ownerUserId, setOwnerUserId] = useState('');
  const [isOpen, setIsOpen] = useState(true);

  const mutation = useMutation({
    mutationFn: () =>
      adminCreateTeam({
        name: name.trim(),
        description: description.trim() || undefined,
        owner_user_id: ownerUserId.trim(),
        is_open: isOpen,
      }),
    onSuccess: (team) => {
      navigate(`/teams/${team.id}`);
    },
    onError: (err: AxiosError<{ message?: string; error?: string }>) => {
      const detail = err.response?.data?.message || err.response?.data?.error || t('errors.createFailed');
      toast.error(detail);
    },
  });

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim() || !ownerUserId.trim()) return;
    mutation.mutate();
  };

  return (
    <div className="mx-auto w-full max-w-2xl">
      <NavLink to="/teams" className="text-sm text-blue-600 hover:text-blue-800">
        {t('detail.back')}
      </NavLink>

      <h1 className="mt-4 text-xl font-semibold text-gray-900 sm:text-2xl">{t('create.title')}</h1>

      <form
        onSubmit={handleSubmit}
        className="mt-6 space-y-4 rounded-lg bg-white p-4 shadow-sm ring-1 ring-gray-200 sm:p-6"
      >
        <div>
          <label className="block text-sm font-medium text-gray-700">{t('create.nameLabel')}</label>
          <input
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={t('create.namePlaceholder')}
            maxLength={100}
            required
            className="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none"
          />
        </div>

        <div>
          <label className="block text-sm font-medium text-gray-700">{t('create.descLabel')}</label>
          <textarea
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            rows={3}
            className="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none"
          />
        </div>

        <div>
          <label className="block text-sm font-medium text-gray-700">{t('create.ownerLabel')}</label>
          <input
            type="text"
            value={ownerUserId}
            onChange={(e) => setOwnerUserId(e.target.value)}
            placeholder="00000000-0000-4000-8000-000000000000"
            required
            className="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 font-mono text-xs focus:border-blue-500 focus:outline-none"
          />
          <p className="mt-1 text-xs text-gray-500">{t('create.ownerHelp')}</p>
        </div>

        <div className="flex items-center gap-2">
          <input
            id="is_open"
            type="checkbox"
            checked={isOpen}
            onChange={(e) => setIsOpen(e.target.checked)}
            className="h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500"
          />
          <label htmlFor="is_open" className="text-sm text-gray-700">
            {t('create.isOpenLabel')}
          </label>
        </div>

        <div className="flex flex-col gap-2 pt-2 sm:flex-row sm:items-center">
          <button
            type="submit"
            disabled={mutation.isPending || !name.trim() || !ownerUserId.trim()}
            className="w-full rounded-md bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700 disabled:opacity-50 sm:w-auto"
          >
            {t('create.submitBtn')}
          </button>
          <button
            type="button"
            onClick={() => navigate('/teams')}
            className="w-full rounded-md border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50 sm:w-auto"
          >
            {t('create.cancelBtn')}
          </button>
        </div>
      </form>
    </div>
  );
}
