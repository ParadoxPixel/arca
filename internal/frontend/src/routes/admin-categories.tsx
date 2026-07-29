import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronDown, ChevronUp, Plus, Settings2, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogForm,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Field, Input, Textarea } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { EmptyState, ErrorBlock, LoadingBlock, PageHeading } from '@/components/ui/feedback'
import { ApiError, del, patch, post, put, type Category } from '@/lib/api'
import { categoriesQuery } from '@/lib/categories'
import { useSession } from '@/lib/session'
import { plural } from '@/lib/utils'

const iconAction =
  'inline-flex size-8 items-center justify-center rounded-full text-faint transition-colors hover:bg-accent hover:text-foreground disabled:pointer-events-none disabled:opacity-30'

function useCategoryRefresh() {
  const queryClient = useQueryClient()

  return async () => {
    await queryClient.invalidateQueries({ queryKey: ['categories'] })
    await queryClient.invalidateQueries({ queryKey: ['repositories'] })
  }
}

function CreateCategoryDialog() {
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const refresh = useCategoryRefresh()

  const mutation = useMutation({
    mutationFn: () => post('/categories', { name, description }),
    onSuccess: async () => {
      await refresh()
      setOpen(false)
      setName('')
      setDescription('')
    },
  })

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm">
          <Plus /> New category
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogForm
          onSubmit={(event: FormEvent) => {
            event.preventDefault()
            mutation.mutate()
          }}
        >
          <DialogHeader>
            <DialogTitle>New category</DialogTitle>
            <DialogDescription>
              Categories group repositories in the sidebar and the index. They never appear in a
              Maven URL.
            </DialogDescription>
          </DialogHeader>
          <DialogBody>
            {mutation.error ? <ErrorBlock message={(mutation.error as ApiError).message} /> : null}
            <Field label="Name">
              <Input
                value={name}
                onChange={(event) => setName(event.target.value)}
                placeholder="Internal"
                autoFocus
              />
            </Field>
            <Field label="Description">
              <Textarea
                value={description}
                onChange={(event) => setDescription(event.target.value)}
              />
            </Field>
          </DialogBody>
          <DialogFooter>
            <Button type="submit" disabled={!name.trim() || mutation.isPending}>
              Create category
            </Button>
          </DialogFooter>
        </DialogForm>
      </DialogContent>
    </Dialog>
  )
}

function EditCategoryDialog({ category }: { category: Category }) {
  const [open, setOpen] = useState(false)
  const [name, setName] = useState(category.name)
  const [description, setDescription] = useState(category.description)
  const refresh = useCategoryRefresh()

  const mutation = useMutation({
    mutationFn: () => patch(`/categories/${category.id}`, { name, description }),
    onSuccess: async () => {
      await refresh()
      setOpen(false)
    },
  })

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <button type="button" className={iconAction} aria-label={`Rename ${category.name}`}>
          <Settings2 className="size-3.5" />
        </button>
      </DialogTrigger>
      <DialogContent>
        <DialogForm
          onSubmit={(event: FormEvent) => {
            event.preventDefault()
            mutation.mutate()
          }}
        >
          <DialogHeader>
            <DialogTitle>{category.name}</DialogTitle>
            <DialogDescription>
              Renaming updates every repository in this category at once.
            </DialogDescription>
          </DialogHeader>
          <DialogBody>
            {mutation.error ? <ErrorBlock message={(mutation.error as ApiError).message} /> : null}
            <Field label="Name">
              <Input value={name} onChange={(event) => setName(event.target.value)} autoFocus />
            </Field>
            <Field label="Description">
              <Textarea
                value={description}
                onChange={(event) => setDescription(event.target.value)}
              />
            </Field>
          </DialogBody>
          <DialogFooter>
            <Button type="submit" disabled={!name.trim() || mutation.isPending}>
              Save changes
            </Button>
          </DialogFooter>
        </DialogForm>
      </DialogContent>
    </Dialog>
  )
}

function DeleteCategoryDialog({ category }: { category: Category }) {
  const [open, setOpen] = useState(false)
  const refresh = useCategoryRefresh()

  const mutation = useMutation({
    mutationFn: () => del(`/categories/${category.id}`),
    onSuccess: async () => {
      await refresh()
      setOpen(false)
    },
  })

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <button
          type="button"
          className="inline-flex size-8 items-center justify-center rounded-full text-faint transition-colors hover:bg-accent hover:text-destructive"
          aria-label={`Delete ${category.name}`}
        >
          <Trash2 className="size-3.5" />
        </button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Delete {category.name}</DialogTitle>
          <DialogDescription>
            {category.repositories === 0
              ? 'This category is empty.'
              : `Its ${plural(category.repositories, 'repository', 'repositories')} stay exactly where they are, listed as uncategorized. Nothing is deleted and no Maven URL changes.`}
          </DialogDescription>
        </DialogHeader>
        <DialogBody>
          {mutation.error ? <ErrorBlock message={(mutation.error as ApiError).message} /> : null}
        </DialogBody>
        <DialogFooter>
          <Button variant="destructive" disabled={mutation.isPending} onClick={() => mutation.mutate()}>
            Delete category
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function AdminCategoriesRoute() {
  const { user } = useSession()
  const { data, error, isLoading } = useQuery(categoriesQuery)
  const refresh = useCategoryRefresh()

  const reorder = useMutation({
    mutationFn: (ids: string[]) => put('/categories/order', { ids }),
    onSuccess: refresh,
  })

  if (user?.role !== 'admin') return <ErrorBlock message="Administrator access required" />
  if (isLoading) return <LoadingBlock label="Loading categories" />
  if (error) return <ErrorBlock message={(error as Error).message} />

  const categories = data?.categories ?? []

  const move = (index: number, delta: number) => {
    const ordered = categories.map((category) => category.id)
    const [moved] = ordered.splice(index, 1)
    if (moved === undefined) return
    ordered.splice(index + delta, 0, moved)
    reorder.mutate(ordered)
  }

  return (
    <>
      <PageHeading
        eyebrow="Manage"
        title="Categories"
        description="Group repositories for the sidebar and the index. Categories are labels only, so they never change a repository's Maven URL."
        actions={<CreateCategoryDialog />}
      />

      {reorder.error ? <ErrorBlock message={(reorder.error as ApiError).message} /> : null}

      {categories.length === 0 ? (
        <EmptyState
          title="No categories yet"
          description="Every repository is listed together until you create one. Adding a category does not move anything: assign repositories to it from their settings."
        />
      ) : (
        <Card>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-12">#</TableHead>
                <TableHead>Name</TableHead>
                <TableHead className="w-32">Repositories</TableHead>
                <TableHead className="w-40" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {categories.map((category, index) => (
                <TableRow key={category.id}>
                  <TableCell className="font-mono text-xs tabular-nums text-faint">
                    {index + 1}
                  </TableCell>
                  <TableCell className="py-3">
                    <p className="text-sm font-medium">{category.name}</p>
                    {category.description ? (
                      <p className="mt-0.5 max-w-md truncate text-xs text-muted-foreground">
                        {category.description}
                      </p>
                    ) : null}
                  </TableCell>
                  <TableCell className="font-mono text-xs tabular-nums text-muted-foreground">
                    {category.repositories}
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center justify-end gap-0.5">
                      <button
                        type="button"
                        className={iconAction}
                        aria-label={`Move ${category.name} up`}
                        disabled={index === 0 || reorder.isPending}
                        onClick={() => move(index, -1)}
                      >
                        <ChevronUp className="size-3.5" />
                      </button>
                      <button
                        type="button"
                        className={iconAction}
                        aria-label={`Move ${category.name} down`}
                        disabled={index === categories.length - 1 || reorder.isPending}
                        onClick={() => move(index, 1)}
                      >
                        <ChevronDown className="size-3.5" />
                      </button>
                      <EditCategoryDialog category={category} />
                      <DeleteCategoryDialog category={category} />
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
      )}
    </>
  )
}
