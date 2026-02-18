import { useTranslation } from 'react-i18next';
import Badge from '../ui/Badge';

export default function StatusBadge({ active }: { active: boolean }) {
  const { t } = useTranslation();
  return (
    <Badge variant={active ? 'green' : 'red'}>
      {active ? t('status.active') : t('status.inactive')}
    </Badge>
  );
}
