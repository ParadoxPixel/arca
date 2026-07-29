import { createContext, useContext, useEffect, type ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type Bootstrap } from '@/lib/api'

const SessionContext = createContext<Bootstrap | null>(null)

export function SessionProvider({
  children,
  fallback,
}: {
  children: ReactNode
  fallback: ReactNode
}) {
  const { data } = useQuery({
    queryKey: ['bootstrap'],
    queryFn: () => api<Bootstrap>('/bootstrap'),
    staleTime: 30_000,
  })

  useEffect(() => {
    if (data?.instanceName) document.title = data.instanceName
  }, [data?.instanceName])

  useEffect(() => {
    if (data?.themeColor) document.documentElement.style.setProperty('--brand', data.themeColor)
  }, [data?.themeColor])

  if (!data) return <>{fallback}</>
  return <SessionContext.Provider value={data}>{children}</SessionContext.Provider>
}

export function useSession(): Bootstrap {
  const session = useContext(SessionContext)
  if (!session) throw new Error('useSession must be used inside SessionProvider')
  return session
}

export function useRefreshSession(): () => Promise<void> {
  const queryClient = useQueryClient()
  return async () => {
    await queryClient.invalidateQueries({ queryKey: ['bootstrap'] })
  }
}
