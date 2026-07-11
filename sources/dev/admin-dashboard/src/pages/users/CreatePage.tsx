import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router';
import { ArrowLeft } from 'lucide-react';
import { createUser } from '../../api/admin';
import type { UserType } from '../../api/types';
import Spinner from '../../components/ui/Spinner';
import toast from 'react-hot-toast';
import { isAxiosError } from 'axios';

export default function UserCreatePage() {
  const { t } = useTranslation('users');
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [name, setName] = useState('');
  const [role, setRole] = useState('user');
  const [userType, setUserType] = useState<UserType>('regular');
  const [birthday, setBirthday] = useState('');
  const [gender, setGender] = useState('');
  const [heightCm, setHeightCm] = useState('');
  const [weightKg, setWeightKg] = useState('');
  const [error, setError] = useState('');

  const mutation = useMutation({
    mutationFn: createUser,
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ['users'] });
      toast.success(t('create.success'));
      navigate(`/users/${data.id}`);
    },
    onError: (err) => {
      if (isAxiosError(err) && err.response?.data?.error) {
        setError(err.response.data.error);
      } else {
        setError(String(err));
      }
    },
  });

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    setError('');
    const custom_attributes: Record<string, string | number> = {};
    if (birthday) custom_attributes.birthday = birthday;
    if (gender) custom_attributes.gender = gender;
    if (heightCm) custom_attributes.height_cm = Number(heightCm);
    if (weightKg) custom_attributes.weight_kg = Number(weightKg);

    mutation.mutate({
      email,
      password,
      name: name || undefined,
      role,
      user_type: userType,
      custom_attributes: Object.keys(custom_attributes).length ? custom_attributes : undefined,
    });
  };

  return (
    <div className="mx-auto w-full max-w-lg">
      <button
        onClick={() => navigate('/users')}
        className="flex items-center gap-1 text-sm text-gray-500 hover:text-gray-700"
      >
        <ArrowLeft size={16} />
        {t('common:actions.back')}
      </button>

      <h1 className="mt-4 text-xl font-semibold text-gray-900 sm:text-2xl">{t('create.title')}</h1>

      <form onSubmit={handleSubmit} className="mt-6 space-y-4 rounded-lg bg-white p-4 shadow-sm ring-1 ring-gray-200 sm:p-6">
        {error && (
          <div className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>
        )}

        <div>
          <label className="block text-sm font-medium text-gray-700">{t('create.email')}</label>
          <input
            type="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder={t('create.emailPlaceholder')}
            className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
          />
        </div>

        <div>
          <label className="block text-sm font-medium text-gray-700">{t('create.password')}</label>
          <input
            type="password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder={t('create.passwordPlaceholder')}
            className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
          />
        </div>

        <div>
          <label className="block text-sm font-medium text-gray-700">{t('create.name')}</label>
          <input
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={t('create.namePlaceholder')}
            className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
          />
        </div>

        <div>
          <label className="block text-sm font-medium text-gray-700">{t('create.role')}</label>
          <select
            value={role}
            onChange={(e) => setRole(e.target.value)}
            className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
          >
            <option value="user">{t('role.user')}</option>
            <option value="admin">{t('role.admin')}</option>
          </select>
        </div>

        <div>
          <label className="block text-sm font-medium text-gray-700">{t('create.userType')}</label>
          <select
            value={userType}
            onChange={(e) => setUserType(e.target.value as UserType)}
            className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
          >
            <option value="regular">{t('userType.regular')}</option>
            <option value="testing">{t('userType.testing')}</option>
          </select>
        </div>

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div>
            <label className="block text-sm font-medium text-gray-700">{t('attributes.birthday')}</label>
            <input
              type="date"
              value={birthday}
              onChange={(e) => setBirthday(e.target.value)}
              className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700">{t('attributes.gender')}</label>
            <select
              value={gender}
              onChange={(e) => setGender(e.target.value)}
              className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
            >
              <option value="">{t('attributes.unspecified')}</option>
              <option value="female">{t('attributes.genderOptions.female')}</option>
              <option value="male">{t('attributes.genderOptions.male')}</option>
              <option value="other">{t('attributes.genderOptions.other')}</option>
            </select>
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700">{t('attributes.heightCm')}</label>
            <input
              type="number"
              min="0"
              step="0.1"
              value={heightCm}
              onChange={(e) => setHeightCm(e.target.value)}
              placeholder={t('attributes.heightPlaceholder')}
              className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700">{t('attributes.weightKg')}</label>
            <input
              type="number"
              min="0"
              step="0.1"
              value={weightKg}
              onChange={(e) => setWeightKg(e.target.value)}
              placeholder={t('attributes.weightPlaceholder')}
              className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
            />
          </div>
        </div>

        <div className="flex justify-end">
          <button
            type="submit"
            disabled={mutation.isPending}
            className="flex w-full items-center justify-center gap-2 rounded-md bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700 disabled:opacity-50 sm:w-auto"
          >
            {mutation.isPending && <Spinner className="h-4 w-4" />}
            {t('create.submit')}
          </button>
        </div>
      </form>
    </div>
  );
}