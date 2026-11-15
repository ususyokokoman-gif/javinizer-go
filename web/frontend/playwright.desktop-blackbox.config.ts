import { defineConfig, devices } from '@playwright/test';

const baseURL = process.env.JAVINIZER_BLACKBOX_URL;
if (!baseURL) {
	throw new Error('JAVINIZER_BLACKBOX_URL is required');
}

export default defineConfig({
	testDir: './tests/desktop-blackbox',
	fullyParallel: false,
	workers: 1,
	retries: 0,
	timeout: 30_000,
	expect: { timeout: 10_000 },
	use: {
		baseURL,
		trace: 'retain-on-failure',
		screenshot: 'only-on-failure',
		...devices['Desktop Chrome']
	},
	reporter: [['line']]
});
