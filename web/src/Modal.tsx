import { useEffect, useRef, type ReactNode } from 'react'

export default function Modal({
  title,
  onClose,
  children,
}: {
  title: string
  onClose: () => void
  children: ReactNode
}) {
  const reference = useRef<HTMLDialogElement>(null)
  useEffect(() => {
    reference.current?.showModal()
  }, [])
  return (
    <dialog
      ref={reference}
      className="modal"
      aria-labelledby="modal-title"
      onCancel={(event) => {
        event.preventDefault()
        onClose()
      }}
    >
      <div className="modal-header">
        <h2 id="modal-title">{title}</h2>
        <button aria-label="Закрыть" onClick={onClose}>
          ×
        </button>
      </div>
      {children}
    </dialog>
  )
}
