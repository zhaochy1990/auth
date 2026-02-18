import { useEffect } from 'react';
import AppRouter from './router';
import { useAuthStore } from './store/authStore';

export default function App() {
  const hydrate = useAuthStore((s) => s.hydrate);

  useEffect(() => {
    hydrate();
  }, [hydrate]);

  return <AppRouter />;
}
