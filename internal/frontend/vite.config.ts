import { spawn, type ChildProcess } from 'node:child_process'
import path from 'node:path'
import { defineConfig, type Plugin } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

const root = import.meta.dirname
const backendRoot = path.resolve(root, '../..')
const backendURL = 'http://127.0.0.1:8080'

async function waitForBackend(url: string, timeoutMs = 60_000): Promise<void> {
  const started = Date.now()

  while (Date.now() - started < timeoutMs) {
    try {
      const response = await fetch(url, { signal: AbortSignal.timeout(1_000) })
      if (response.status > 0) return
    } catch {
      // The backend is still starting, retry until it answers.
    }
    await new Promise((resolve) => setTimeout(resolve, 250))
  }

  console.warn(`[backend] ${url} did not answer within ${timeoutMs}ms, the API proxy may fail`)
}

function goBackend(): Plugin {
  let backend: ChildProcess | null = null

  const stop = () => {
    backend?.kill('SIGINT')
    backend = null
  }

  return {
    name: 'go-backend',

    async configureServer(server) {
      if (backend) return

      console.log('[backend] starting go run ./cmd/arca --dev')

      backend = spawn('go', ['run', './cmd/arca', '--dev'], {
        cwd: backendRoot,
        stdio: ['ignore', 'inherit', 'inherit'],
        env: { ...process.env, FORCE_COLOR: '1' },
      })

      backend.on('error', (cause) => console.error('[backend] failed to start:', cause))
      backend.on('exit', (code, signal) => {
        console.log(`[backend] exited (code=${code}, signal=${signal})`)
        backend = null
      })

      server.httpServer?.once('close', stop)
      process.once('SIGINT', stop)
      process.once('SIGTERM', stop)

      await waitForBackend(`${backendURL}/api/bootstrap`)
      console.log('[backend] ready')
    },
  }
}

export default defineConfig({
  root,
  plugins: [react(), tailwindcss(), goBackend()],
  resolve: {
    alias: { '@': path.join(root, 'src') },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: false,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes('node_modules')) return
          if (id.includes('react-dom') || id.includes('react-router') || id.includes('/react/')) {
            return 'vendor-react'
          }
          if (id.includes('lucide-react')) return 'vendor-icons'
        },
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': backendURL,
      '/repository': backendURL,
    },
  },
})
