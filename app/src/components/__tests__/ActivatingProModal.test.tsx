import React from 'react';
import TestRenderer, {act} from 'react-test-renderer';
import {ActivatingProModal} from '../ActivatingProModal';
import {getInvoice} from '../../services/payment';

jest.mock('@react-navigation/native', () => ({
  useNavigation: () => ({navigate: jest.fn()}),
}));

jest.mock('react-i18next', () => ({
  useTranslation: () => ({t: (k: string) => k}),
}));

jest.mock('../../services/payment', () => ({
  getInvoice: jest.fn(),
}));

let mockStoreState: any;
jest.mock('../../stores/authStore', () => ({
  useAuthStore: (selector: any) => selector(mockStoreState),
}));

function setStore(over: Partial<any>) {
  mockStoreState = {
    pendingInvoiceId: 'inv_X',
    isActivatingPro: true,
    isAuthenticated: true,
    fetchAccount: jest.fn().mockResolvedValue(undefined),
    stopActivatingPro: jest.fn(),
    ...over,
  };
}

describe('ActivatingProModal polling', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    jest.useFakeTimers();
    setStore({});
  });

  afterEach(() => {
    jest.useRealTimers();
  });

  it('polls /invoices/:id every 2000ms', async () => {
    (getInvoice as jest.Mock).mockResolvedValue({id: 'inv_X', status: 'pending'});
    let component: any;
    await act(async () => {
      component = TestRenderer.create(<ActivatingProModal />);
    });
    // Poll 1 fires immediately.
    await act(async () => {
      await Promise.resolve(); // flush microtasks
    });
    expect(getInvoice).toHaveBeenCalledTimes(1);
    expect(getInvoice).toHaveBeenLastCalledWith('inv_X', false);

    // Advance 2s → poll 2.
    await act(async () => {
      jest.advanceTimersByTime(2000);
      await Promise.resolve();
    });
    expect(getInvoice).toHaveBeenCalledTimes(2);
    await act(async () => {
      component.unmount();
    });
  });

  it('escalates after poll #5 (poll 6 uses ?escalate=true via second arg)', async () => {
    (getInvoice as jest.Mock).mockResolvedValue({id: 'inv_X', status: 'pending'});
    let component: any;
    await act(async () => {
      component = TestRenderer.create(<ActivatingProModal />);
    });
    await act(async () => {
      await Promise.resolve();
    });
    for (let i = 0; i < 6; i += 1) {
      await act(async () => {
        jest.advanceTimersByTime(2000);
        await Promise.resolve();
      });
    }
    const calls = (getInvoice as jest.Mock).mock.calls;
    // Polls 1-5: second arg false; poll 6+: second arg true.
    expect(calls[0][1]).toBe(false);
    expect(calls[4][1]).toBe(false);
    expect(calls[5][1]).toBe(true);
    await act(async () => {
      component.unmount();
    });
  });

  it('calls fetchAccount + stopActivatingPro on status=paid', async () => {
    (getInvoice as jest.Mock).mockResolvedValueOnce({id: 'inv_X', status: 'paid'});
    await act(async () => {
      TestRenderer.create(<ActivatingProModal />);
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(mockStoreState.fetchAccount).toHaveBeenCalled();
    // stopActivatingPro fires after 3s success-display delay.
    await act(async () => {
      jest.advanceTimersByTime(3000);
    });
    expect(mockStoreState.stopActivatingPro).toHaveBeenCalled();
  });

  it('renders nothing when isActivatingPro is false', () => {
    setStore({isActivatingPro: false});
    let tree: any;
    act(() => {
      tree = TestRenderer.create(<ActivatingProModal />);
    });
    expect(tree.toJSON()).toBeNull();
  });
});
