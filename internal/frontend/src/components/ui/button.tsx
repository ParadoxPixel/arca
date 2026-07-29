import * as React from 'react'
import { Slot } from '@radix-ui/react-slot'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

const buttonVariants = cva(
  "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-full font-medium transition-colors disabled:pointer-events-none disabled:opacity-40 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default: 'bg-solid text-solid-foreground hover:bg-solid/88',
        secondary: 'bg-muted text-foreground hover:bg-accent',
        outline: 'border border-border bg-card text-foreground hover:bg-accent',
        ghost: 'text-muted-foreground hover:bg-accent hover:text-foreground',
        soft: 'bg-primary-soft text-primary hover:bg-primary-soft/70',
        destructive: 'bg-destructive text-white hover:bg-destructive/88',
        link: 'text-primary underline-offset-4 hover:underline',
      },
      size: {
        default: 'h-9 px-4 text-sm',
        sm: 'h-8 px-3.5 text-[13px]',
        lg: 'h-11 px-6 text-[15px]',
        icon: 'size-9',
      },
    },
    defaultVariants: { variant: 'default', size: 'default' },
  },
)

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean
}

export function Button({ className, variant, size, asChild, ...props }: ButtonProps) {
  const Comp = asChild ? Slot : 'button'
  return <Comp className={cn(buttonVariants({ variant, size }), className)} {...props} />
}

export function IconAction({
  className,
  tone = 'default',
  ...props
}: React.ComponentProps<'button'> & { tone?: 'default' | 'danger' }) {
  return (
    <button
      type="button"
      className={cn(
        'inline-flex size-8 items-center justify-center rounded-full text-faint transition-colors hover:bg-accent',
        tone === 'danger' ? 'hover:text-destructive' : 'hover:text-foreground',
        'disabled:pointer-events-none disabled:opacity-40',
        className,
      )}
      {...props}
    />
  )
}

export { buttonVariants }
