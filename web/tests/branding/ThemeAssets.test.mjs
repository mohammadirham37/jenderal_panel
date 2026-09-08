import { existsSync, readFileSync } from 'node:fs';
import { test } from 'node:test';
import assert from 'node:assert/strict';

const appHtml = readFileSync(new URL('../../src/app.html', import.meta.url), 'utf8');
const appCss = readFileSync(new URL('../../src/app.css', import.meta.url), 'utf8');

function pngMetadata(relativePath) {
	const assetUrl = new URL(relativePath, import.meta.url);
	assert.equal(existsSync(assetUrl), true, `${relativePath} should exist`);
	const image = readFileSync(assetUrl);
	assert.equal(image.subarray(1, 4).toString('ascii'), 'PNG');
	return {
		width: image.readUInt32BE(16),
		height: image.readUInt32BE(20),
		colorType: image[25]
	};
}

function cssColor(variable) {
	const value = appCss.match(new RegExp(`${variable}:\\s*#([0-9a-f]{6})`, 'i'))?.[1];
	assert.ok(value, `${variable} should be a six-digit hex color`);
	return value;
}

function lightCssColor(variable) {
	const lightTheme = appCss.match(/html\[data-theme=['"]light['"]\]\s*\{([\s\S]*?)\}/)?.[1];
	assert.ok(lightTheme, 'light theme variables should be defined');
	const value = lightTheme.match(new RegExp(`${variable}:\\s*#([0-9a-f]{6})`, 'i'))?.[1];
	assert.ok(value, `${variable} should be a six-digit light theme color`);
	return value;
}

function luminance(hex) {
	const channels = hex.match(/.{2}/g).map((channel) => Number.parseInt(channel, 16) / 255);
	const [red, green, blue] = channels.map((channel) =>
		channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4
	);
	return 0.2126 * red + 0.7152 * green + 0.0722 * blue;
}

function contrast(first, second) {
	const light = Math.max(luminance(first), luminance(second));
	const dark = Math.min(luminance(first), luminance(second));
	return (light + 0.05) / (dark + 0.05);
}

test('publishes favicon metadata for browser and touch devices', () => {
	assert.match(appHtml, /<link rel="icon" type="image\/png" href="\/favicon\.png" \/>/);
	assert.match(appHtml, /<link rel="apple-touch-icon" href="\/apple-touch-icon\.png" \/>/);
});

test('ships square transparent favicon assets', () => {
	assert.deepEqual(pngMetadata('../../static/favicon.png'), {
		width: 64,
		height: 64,
		colorType: 6
	});
	assert.deepEqual(pngMetadata('../../static/apple-touch-icon.png'), {
		width: 180,
		height: 180,
		colorType: 6
	});
});

test('defines the shared deep-slate and teal theme surfaces', () => {
	assert.match(appCss, /--color-gray-900:\s*#07111e/);
	assert.match(appCss, /--color-blue-600:\s*#[0-9a-f]{6}/i);
	assert.match(appCss, /\.login-shell\s*\{/);
	assert.match(appCss, /\.app-shell\s*\{/);
});

test('keeps primary teal actions readable with white text', () => {
	assert.ok(contrast(cssColor('--color-blue-600'), 'ffffff') >= 4.5);
	assert.ok(contrast(cssColor('--color-blue-700'), 'ffffff') >= 4.5);
});

test('keeps muted microcopy readable on panel surfaces', () => {
	assert.ok(contrast(cssColor('--color-gray-400'), cssColor('--color-gray-800')) >= 4.5);
});

test('keeps neutral hover buttons readable with white text', () => {
	assert.ok(contrast(cssColor('--color-gray-500'), 'ffffff') >= 4.5);
});

test('defines an accessible light theme for panel text and surfaces', () => {
	assert.ok(contrast(lightCssColor('--color-gray-100'), lightCssColor('--color-gray-900')) >= 4.5);
	assert.ok(contrast(lightCssColor('--color-gray-200'), lightCssColor('--color-gray-900')) >= 4.5);
	assert.ok(contrast(lightCssColor('--color-gray-400'), lightCssColor('--color-gray-800')) >= 4.5);
	assert.ok(contrast(lightCssColor('--color-blue-400'), lightCssColor('--color-gray-800')) >= 4.5);
	assert.ok(contrast(lightCssColor('--color-red-400'), lightCssColor('--color-gray-800')) >= 4.5);
	assert.ok(contrast(lightCssColor('--color-green-400'), lightCssColor('--color-gray-800')) >= 4.5);
	assert.ok(contrast(lightCssColor('--color-yellow-400'), lightCssColor('--color-gray-800')) >= 4.5);
});

test('keeps light-theme action colors readable with white text', () => {
	assert.ok(contrast(lightCssColor('--color-red-600'), 'ffffff') >= 4.5);
	assert.ok(contrast(lightCssColor('--color-green-600'), 'ffffff') >= 4.5);
	assert.ok(contrast(lightCssColor('--color-yellow-600'), 'ffffff') >= 4.5);
});
