// Form controls with the legacy classes: `.inp`, `.sel`, `.chk`.

import type { InputHTMLAttributes, ReactNode, SelectHTMLAttributes } from 'react';

type InputProps = Omit<InputHTMLAttributes<HTMLInputElement>, 'className'> & {
  mono?: boolean;
  className?: string;
  ref?: React.Ref<HTMLInputElement>;
};

export function TextInput({ mono, className, type = 'text', ...rest }: InputProps) {
  return <input type={type} className={['inp', mono && 'mono', className].filter(Boolean).join(' ')} {...rest} />;
}

export type Option<T extends string> = { value: T; label: string };

type SelectProps<T extends string> = Omit<
  SelectHTMLAttributes<HTMLSelectElement>,
  'className' | 'onChange' | 'value'
> & {
  value: T;
  options: readonly Option<T>[];
  onChange: (value: T) => void;
  className?: string;
};

export function Select<T extends string>({ value, options, onChange, className, ...rest }: SelectProps<T>) {
  return (
    <select
      className={className ? `sel ${className}` : 'sel'}
      value={value}
      onChange={(e) => {
        onChange(e.target.value as T);
      }}
      {...rest}
    >
      {options.map((o) => (
        <option key={o.value} value={o.value}>
          {o.label}
        </option>
      ))}
    </select>
  );
}

type CheckboxProps = Omit<InputHTMLAttributes<HTMLInputElement>, 'type' | 'onChange'> & {
  label: ReactNode;
  onChange: (checked: boolean) => void;
};

export function Checkbox({ label, onChange, ...rest }: CheckboxProps) {
  return (
    <label className="chk">
      <input
        type="checkbox"
        onChange={(e) => {
          onChange(e.target.checked);
        }}
        {...rest}
      />
      <span>{label}</span>
    </label>
  );
}
