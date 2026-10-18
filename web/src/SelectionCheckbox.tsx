import { useEffect, useRef, type ReactNode } from 'react'

export default function SelectionCheckbox({
  checked,
  mixed = false,
  disabled,
  onChange,
  label,
  className = '',
  children,
}: {
  checked: boolean
  mixed?: boolean
  disabled?: boolean
  onChange: () => void
  label: string
  className?: string
  children?: ReactNode
}) {
  const input = useRef<HTMLInputElement>(null)
  useEffect(() => {
    if (input.current) input.current.indeterminate = mixed
  }, [mixed])
  return (
    <label className={`selection-checkbox ${className}`} data-disabled={disabled || undefined}>
      <input
        ref={input}
        type="checkbox"
        aria-label={label}
        checked={checked}
        aria-checked={mixed ? 'mixed' : checked}
        disabled={disabled}
        onChange={onChange}
      />
      <span className="selection-box" aria-hidden="true">
        <svg
          viewBox="0 0 20 20"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          {mixed ? <path d="M5 10h10" /> : <path d="m4.5 10 3.5 3.5 7.5-7.5" />}
        </svg>
      </span>
      {children}
    </label>
  )
}
