import { useState, type KeyboardEvent } from 'react';
import { X } from 'lucide-react';

interface Props {
  value: string[];
  onChange: (value: string[]) => void;
  placeholder?: string;
}

export default function TagInput({ value, onChange, placeholder }: Props) {
  const [input, setInput] = useState('');

  const commitInput = () => {
    const trimmed = input.trim();
    if (trimmed && !value.includes(trimmed)) {
      onChange([...value, trimmed]);
    }
    setInput('');
  };

  const handleKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      commitInput();
    }
    if (e.key === 'Backspace' && !input && value.length > 0) {
      onChange(value.slice(0, -1));
    }
  };

  const remove = (idx: number) => {
    onChange(value.filter((_, i) => i !== idx));
  };

  return (
    <div className="flex flex-wrap items-center gap-1 rounded-md border border-gray-300 px-2 py-1.5 focus-within:border-blue-500 focus-within:ring-1 focus-within:ring-blue-500">
      {value.map((tag, i) => (
        <span key={i} className="inline-flex items-center gap-1 rounded bg-blue-100 px-2 py-0.5 text-sm text-blue-800">
          {tag}
          <button type="button" onClick={() => remove(i)} className="text-blue-600 hover:text-blue-800">
            <X size={12} />
          </button>
        </span>
      ))}
      <input
        type="text"
        className="min-w-[120px] flex-1 border-0 p-0 text-sm outline-none"
        value={input}
        onChange={(e) => setInput(e.target.value)}
        onKeyDown={handleKeyDown}
        onBlur={commitInput}
        placeholder={value.length === 0 ? placeholder : ''}
      />
    </div>
  );
}
