import { useState } from 'react'
import { BrandSplotch } from '@/components/brand'
import { Textarea } from '@/components/ui/input'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Markdown } from '@/lib/markdown'

export const welcomeTextLimit = 8000

export function WelcomeEditor({
  value,
  onChange,
  rows = 12,
}: {
  value: string
  onChange: (value: string) => void
  rows?: number
}) {
  const [tab, setTab] = useState('write')

  return (
    <Tabs value={tab} onValueChange={setTab} className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <TabsList>
          <TabsTrigger value="write">Write</TabsTrigger>
          <TabsTrigger value="preview">Preview</TabsTrigger>
        </TabsList>
        <p className="font-mono text-[11px] text-faint">
          {value.length} / {welcomeTextLimit}
        </p>
      </div>

      <TabsContent value="write" className="mt-0">
        <Textarea
          value={value}
          onChange={(event) => onChange(event.target.value)}
          rows={rows}
          maxLength={welcomeTextLimit}
          spellCheck
          placeholder="## Welcome&#10;&#10;What this instance is for."
          className="resize-y font-mono text-[13px] leading-relaxed"
        />
        <p className="mt-2 text-xs text-muted-foreground">
          Markdown: <code className="font-mono">#</code> headings,{' '}
          <code className="font-mono">**bold**</code>, <code className="font-mono">*italic*</code>,{' '}
          <code className="font-mono">`code`</code>, <code className="font-mono">- lists</code>,{' '}
          <code className="font-mono">&gt; quotes</code> and{' '}
          <code className="font-mono">[links](https://example.com)</code>.
        </p>
      </TabsContent>

      <TabsContent value="preview" className="mt-0">
        <div className="relative overflow-hidden rounded-2xl bg-muted px-6 py-6 text-foreground">
          <BrandSplotch className="left-full top-0 opacity-20 dark:opacity-25" />
          <div className="relative">
            {value.trim() ? (
              <Markdown source={value} className="max-w-2xl" />
            ) : (
              <p className="text-[13px] text-muted-foreground">
                Nothing to show. The landing page skips the hero when the welcome text is empty.
              </p>
            )}
          </div>
        </div>
      </TabsContent>
    </Tabs>
  )
}
