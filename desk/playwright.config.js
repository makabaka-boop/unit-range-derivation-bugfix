import { defineConfig } from '@playwright/test'

// 本地跑 e2e 时自动拉起 Go 服务与 Vite 开发服务器；
// Docker Compose 场景下两个服务已经在运行，reuseExistingServer 会直接复用。
export default defineConfig({
  testDir: './e2e',
  timeout: 30000,
  fullyParallel: false,
  reporter: [['list']],
  use: {
    baseURL: process.env.E2E_BASE_URL || 'http://localhost:5173',
    trace: 'on-first-retry'
  },
  webServer: [
    {
      command: 'go run ./cmd/units',
      cwd: '../units',
      port: 8080,
      timeout: 300000,
      reuseExistingServer: true,
      env: {
        PATH: process.env.PATH,
        HOME: process.env.HOME || '/tmp',
        GOCACHE: process.env.GOCACHE || '/tmp/gocache',
        GOPATH: process.env.GOPATH || '/tmp/gopath',
        GOTOOLCHAIN: 'local',
        CGO_ENABLED: '0',
        ADDR: ':8080'
      }
    },
    {
      // 用生产构建跑测试，避免 Vite 开发服务器首次依赖优化造成冷启动超时。
      command: 'npm run build && npm run preview',
      port: 5173,
      timeout: 300000,
      reuseExistingServer: true,
      env: {
        UNITS_URL: 'http://localhost:8080'
      }
    }
  ]
})
