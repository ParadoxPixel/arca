import { Field } from '@/components/ui/input'
import { cn } from '@/lib/utils'

export const themeColorPresets = [
  '#4f46e5',
  '#2563eb',
  '#0891b2',
  '#0d9488',
  '#16a34a',
  '#ca8a04',
  '#ea580c',
  '#dc2626',
  '#db2777',
  '#7c3aed',
]

const hex = /^#[0-9a-f]{6}$/i

export function isThemeColor(value: string): boolean {
  return hex.test(value)
}

export function ThemeColorField({
  value,
  onChange,
  hint = 'Used for the accent behind the navigation. Any hex colour works.',
}: {
  value: string
  onChange: (value: string) => void
  hint?: string
}) {
  const valid = isThemeColor(value)

  return (
    <Field label="Theme color" hint={hint}>
      <div className="space-y-3">
        <div className="flex flex-wrap gap-2">
          {themeColorPresets.map((preset) => (
            <button
              key={preset}
              type="button"
              onClick={() => onChange(preset)}
              aria-label={preset}
              aria-pressed={preset.toLowerCase() === value.toLowerCase()}
              className={cn(
                'size-7 rounded-full ring-offset-2 ring-offset-card transition-transform hover:scale-110',
                preset.toLowerCase() === value.toLowerCase() ? 'ring-2 ring-foreground' : '',
              )}
              style={{ background: preset }}
            />
          ))}
        </div>

        <div className="flex items-center gap-2">
          <input
            type="color"
            value={valid ? value : '#000000'}
            onChange={(event) => onChange(event.target.value)}
            aria-label="Pick a theme color"
            className="size-9 shrink-0 cursor-pointer rounded-xl border border-border bg-card p-1"
          />
          <input
            value={value}
            onChange={(event) => onChange(event.target.value)}
            aria-label="Theme color hex"
            spellCheck={false}
            className={cn(
              'h-9 w-32 rounded-xl border bg-card px-3 font-mono text-[13px] uppercase outline-none transition-colors',
              valid ? 'border-border focus-visible:border-primary' : 'border-destructive',
            )}
          />
          {valid ? null : (
            <span className="text-xs text-destructive">Use a hex colour like #4F46E5</span>
          )}
        </div>
      </div>
    </Field>
  )
}
