import { defineConfig } from '@playwright/test'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { baseURL, e2eRoot, isolatedEnv } from './e2e/support/server'

export default defineConfig({
  testDir: './e2e',
  testMatch: /.*\.e2e\.ts$/,
  workers: 1,
  fullyParallel: false,
  retries: 0,
  updateSnapshots: 'none',
  forbidOnly: true,
  timeout: 30_000,
  outputDir: join(tmpdir(), 'reviewer-e2e-results'),
  reporter: 'list',
  globalSetup: './e2e/global-setup.ts',
  globalTeardown: './e2e/global-teardown.ts',
  use: {
    baseURL: baseURL(),
    locale: 'ja-JP', timezoneId: 'Asia/Tokyo', colorScheme: 'light',
    trace: 'retain-on-failure', screenshot: 'only-on-failure',
    launchOptions: { env: Object.fromEntries(Object.entries(isolatedEnv(e2eRoot)).filter((entry): entry is [string, string] => entry[1] !== undefined)) },
  },
  expect: { toHaveScreenshot: { maxDiffPixels: 0, animations: 'disabled', caret: 'hide' } },
  projects: [
    { name: 'desktop', testMatch: /desktop\/.*\.e2e\.ts$/, use: { viewport: { width: 1440, height: 900 } } },
    { name: 'boundary', testMatch: /boundary\/.*\.e2e\.ts$/, use: { viewport: { width: 839, height: 900 } } },
    ...[
      { name: 'mobile-360', width: 360, height: 800, scale: 3 },
      { name: 'mobile-412', width: 412, height: 915, scale: 2.625 },
      { name: 'tablet-768', width: 768, height: 1024, scale: 2 },
      { name: 'tablet-landscape', width: 1024, height: 768, scale: 2 },
    ].map(({ name, width, height, scale }) => ({
      name, testMatch: /mobile\/.*\.e2e\.ts$/,
      use: {
        viewport: { width, height }, isMobile: true, hasTouch: true, deviceScaleFactor: scale,
        userAgent: 'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/153.0.0.0 Mobile Safari/537.36',
      },
    })),
    { name: 'sse', testMatch: /sse\/.*\.e2e\.ts$/, use: { viewport: { width: 1440, height: 900 } } },
    { name: 'spike', testMatch: /spike\/.*\.e2e\.ts$/ },
  ],
})
