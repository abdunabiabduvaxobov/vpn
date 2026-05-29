// app/src/components/__tests__/ActivatingProModal.test.tsx
// Phase 5 Wave 0 scaffold — Wave 3 fills in implementations.
// Tracks: 05-VALIDATION.md task 5-UI-03, 5-UI-04.

describe.skip('ActivatingProModal polling', () => {
  it('polls every 2000ms (fake timers)', () => {
    // Wave 3: jest.useFakeTimers(); advance 2000ms → one getInvoice call per tick
  });

  it('escalates after poll #5 (poll 6 url contains ?escalate=true)', () => {
    // Wave 3: advance past 10s → poll #6 onward appends ?escalate=true
  });

  it('times out at 30s and shows takingLonger state', () => {
    // Wave 3: advance 30s with no paid status → modal switches to payment.takingLonger state
  });

  it('closes modal + calls fetchAccount on status=paid', () => {
    // Wave 3: getInvoice resolves status='paid' → modal closes, authStore.fetchAccount called
  });

  it('navigates to Account on status=failed', () => {
    // Wave 3: getInvoice resolves status='failed' → navigation to Account screen
  });
});
