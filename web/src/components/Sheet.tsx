import * as Dialog from "@radix-ui/react-dialog";
import type { ReactNode } from "react";

/** A right-side panel on desktop, a bottom sheet on mobile — used for the
 * bot inspector so it never fights the canvas for space on a small screen. */
export function Sheet({
  open,
  onOpenChange,
  title,
  subtitle,
  children,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  subtitle?: string;
  children: ReactNode;
}) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-black/50 data-[state=open]:animate-[fade-in_.15s_ease-out]" />
        <Dialog.Content
          className="fixed z-50 flex flex-col overflow-hidden border-edge bg-panel
            inset-x-0 bottom-0 max-h-[85vh] rounded-t-xl border-t
            sm:inset-y-0 sm:left-auto sm:right-0 sm:bottom-auto sm:h-full sm:w-[380px] sm:max-h-none sm:rounded-none sm:border-l sm:border-t-0"
        >
          <div className="flex items-start justify-between border-b border-edge px-5 py-4">
            <div>
              <Dialog.Title className="font-display text-base font-semibold text-ink">
                {title}
              </Dialog.Title>
              {subtitle && (
                <Dialog.Description className="mt-0.5 text-xs text-muted">
                  {subtitle}
                </Dialog.Description>
              )}
            </div>
            <Dialog.Close className="rounded p-1 text-muted hover:bg-white/5 hover:text-ink">
              ✕
            </Dialog.Close>
          </div>
          <div className="flex-1 overflow-auto px-5 py-4">{children}</div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
