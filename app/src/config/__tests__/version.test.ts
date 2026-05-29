// app/src/config/__tests__/version.test.ts
// Phase 5 Wave 0 — guards against version drift.
// Tracks: 05-VALIDATION.md task 5-VER-01.
// INTENTIONALLY RED until Wave 4 bumps APP_VERSION to '2.2.0'.

import {APP_VERSION} from '../version';

describe('APP_VERSION', () => {
  it('matches package.json version 2.2.0', () => {
    expect(APP_VERSION).toBe('2.2.0');
  });
});
