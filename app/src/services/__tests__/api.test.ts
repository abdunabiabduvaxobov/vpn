// app/src/services/__tests__/api.test.ts
// Phase 5 Wave 0 scaffold — Wave 2 fills in implementations.
// Tracks: 05-VALIDATION.md task 5-SVC-09.

describe.skip('axios 401 interceptor', () => {
  it('short-circuits refresh for /auth/* endpoints', () => {
    // Wave 2: a 401 from /auth/apple or /auth/google must NOT trigger the refresh loop
  });

  it('respects _skipAuthRefresh: true on request config', () => {
    // Wave 2: a request flagged {_skipAuthRefresh: true} must bypass the 401 refresh retry
  });
});
