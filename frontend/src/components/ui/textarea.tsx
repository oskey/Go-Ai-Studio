import * as React from "react"

import { cn } from "@/lib/utils"

export interface TextareaProps
  extends React.TextareaHTMLAttributes<HTMLTextAreaElement> {
  autoResize?: boolean
}

const Textarea = React.forwardRef<HTMLTextAreaElement, TextareaProps>(
  ({ className, autoResize = true, onInput: onInputProp, ...props }, ref) => {
    // Auto-resize logic
    const handleInput: React.FormEventHandler<HTMLTextAreaElement> = (e) => {
      if (autoResize) {
        const target = e.currentTarget;
        target.style.height = 'auto';
        target.style.height = `${target.scrollHeight}px`;
      }
      (onInputProp as React.FormEventHandler<HTMLTextAreaElement> | undefined)?.(e);
    };

    return (
      <textarea
        {...props}
        className={cn(
          "flex min-h-[80px] w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 transition-all duration-200 ease-in-out",
          autoResize ? "resize-y overflow-hidden" : "resize-none overflow-y-auto",
          className
        )}
        ref={ref}
        onInput={handleInput}
      />
    )
  }
)
Textarea.displayName = "Textarea"

export { Textarea }
