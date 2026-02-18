const variants: Record<string, string> = {
  green: 'bg-green-100 text-green-800',
  red: 'bg-red-100 text-red-800',
  gray: 'bg-gray-100 text-gray-800',
  blue: 'bg-blue-100 text-blue-800',
  yellow: 'bg-yellow-100 text-yellow-800',
};

interface BadgeProps {
  variant?: keyof typeof variants;
  children: React.ReactNode;
}

export default function Badge({ variant = 'gray', children }: BadgeProps) {
  return (
    <span className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ${variants[variant] || variants.gray}`}>
      {children}
    </span>
  );
}
