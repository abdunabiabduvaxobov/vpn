import React from 'react';
import TestRenderer, {act} from 'react-test-renderer';
import {PaymentScreen} from '../PaymentScreen';

jest.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (k: string) => {
      const map: Record<string, string> = {
        'payment.upgrade.cta': 'Upgrade to Pro at risevpn.com',
        'payment.upgrade.alreadyPaid': 'Already paid? Refresh',
        'payment.upgrade.currentPlanLabel': 'Your current plan',
        'payment.upgrade.proIncludesHeading': 'Pro includes',
        'payment.upgrade.freeLimits.devices': '1 device',
        'payment.upgrade.freeLimits.data': '10 GB / month',
        'payment.upgrade.freeLimits.servers': '5 server locations',
        'payment.upgrade.proFeatures.speed': 'Unlimited speed',
        'payment.upgrade.proFeatures.servers': 'All servers worldwide',
        'payment.upgrade.proFeatures.devices': 'Up to 5 devices',
        'payment.upgrade.proFeatures.ads': 'No ads',
        'payment.upgrade.proActiveTitle': 'Your current plan: Pro',
      };
      return map[k] ?? k;
    },
  }),
}));

jest.mock('i18next', () => ({language: 'en'}));

jest.mock('../../stores/authStore', () => ({
  useAuthStore: (selector: any) =>
    selector({
      user: {
        id: 'u1',
        full_name: 'T',
        subscription_tier: 'free',
        subscription_expires_at: null,
        created_at: '',
        auth_provider: 'guest',
      },
      fetchAccount: jest.fn(),
    }),
}));

jest.mock('../../services/payment', () => ({
  upgradeUrlForLocale: jest.fn(() => 'https://risevpn.com/en/pricing?return=app'),
}));

function renderScreen() {
  let tree: TestRenderer.ReactTestRenderer;
  act(() => {
    tree = TestRenderer.create(<PaymentScreen />);
  });
  return JSON.stringify(tree!.toJSON());
}

describe('PaymentScreen informational layout', () => {
  it('renders the locked CTA copy exactly "Upgrade to Pro at risevpn.com"', () => {
    const text = renderScreen();
    expect(text).toContain('Upgrade to Pro at risevpn.com');
  });

  it('renders no $ or € or /mo price text', () => {
    const text = renderScreen();
    expect(text).not.toMatch(/\$\d/);
    expect(text).not.toMatch(/€\d/);
    expect(text).not.toContain('/mo');
    // Note: '10 GB / month' is allowed (D-14 free-tier limit copy). The
    // prohibition is restricted to currency-symbol price patterns.
  });

  it('renders the tertiary "Already paid? Refresh" link', () => {
    const text = renderScreen();
    expect(text).toContain('Already paid? Refresh');
  });

  it('renders no Telegram CTA (Stripe-era flow removed)', () => {
    const text = renderScreen();
    expect(text.toLowerCase()).not.toContain('telegram');
  });
});
