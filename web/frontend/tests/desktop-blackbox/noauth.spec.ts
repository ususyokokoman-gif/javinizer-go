import { test, expect } from '@playwright/test';
import path from 'node:path';
import { writeFile } from 'node:fs/promises';

const evidenceDir = process.env.JAVINIZER_BLACKBOX_EVIDENCE_DIR;
if (!evidenceDir) {
	throw new Error('JAVINIZER_BLACKBOX_EVIDENCE_DIR is required');
}

test('desktop localhost opens the main UI without login', async ({ page }) => {
	const authCalls: string[] = [];
	page.on('request', (request) => {
		const pathname = new URL(request.url()).pathname;
		if (pathname.startsWith('/api/v1/auth/')) authCalls.push(`${request.method()} ${pathname}`);
	});

	await page.goto('/', { waitUntil: 'networkidle' });

	await expect(page.locator('nav')).toBeVisible();
	await expect(page.locator('a[href="/browse"]')).toBeVisible();
	await expect(page.locator('#login-username')).toHaveCount(0);
	await expect(page.locator('#login-password')).toHaveCount(0);
	await expect(page.locator('form')).not.toContainText(/sign in|ログイン/i);

	const logoutButtons = page.locator('button[title]').filter({ hasText: /logout|ログアウト/i });
	await expect(logoutButtons).toHaveCount(0);

	const setupOrLogin = authCalls.filter((call) => /POST \/api\/v1\/auth\/(setup|login)$/.test(call));
	expect(setupOrLogin).toEqual([]);

	const authEvidence = authCalls.join('\n') + '\n';
	await writeFile(path.join(evidenceDir, 'desktop-noauth-auth-requests.txt'), authEvidence, 'utf8');
	await page.screenshot({
		path: path.join(evidenceDir, 'desktop-noauth-main-ui.png'),
		fullPage: true
	});

	await test.info().attach('auth-requests', {
		body: Buffer.from(authEvidence),
		contentType: 'text/plain'
	});
});
