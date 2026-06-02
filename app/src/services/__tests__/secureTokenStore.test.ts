// HARD-16 / SC#5 — tests for the Keychain-backed secure token store (D-11).
// Proves set/get/clear route through react-native-keychain under a stable
// service key and that get tolerates missing / malformed entries.

jest.mock('react-native-keychain', () => ({
  setGenericPassword: jest.fn().mockResolvedValue(true),
  getGenericPassword: jest.fn().mockResolvedValue(false),
  resetGenericPassword: jest.fn().mockResolvedValue(true),
}));

import * as Keychain from 'react-native-keychain';
import {secureTokenStore} from '../secureTokenStore';

const tokens = {access_token: 'AT', refresh_token: 'RT', expires_in: 300};

describe('secureTokenStore', () => {
  beforeEach(() => jest.clearAllMocks());

  it('setTokens writes the JSON pair under the stable risevpn.auth service', async () => {
    await secureTokenStore.setTokens(tokens);
    expect(Keychain.setGenericPassword).toHaveBeenCalledWith(
      'risevpn-auth',
      JSON.stringify(tokens),
      {service: 'risevpn.auth'},
    );
    expect(secureTokenStore.SERVICE).toBe('risevpn.auth');
  });

  it('getTokens parses the stored password back into the token pair', async () => {
    (Keychain.getGenericPassword as jest.Mock).mockResolvedValueOnce({
      username: 'risevpn-auth',
      password: JSON.stringify(tokens),
      service: 'risevpn.auth',
    });
    await expect(secureTokenStore.getTokens()).resolves.toEqual(tokens);
    expect(Keychain.getGenericPassword).toHaveBeenCalledWith({
      service: 'risevpn.auth',
    });
  });

  it('getTokens returns null when there is no Keychain entry', async () => {
    (Keychain.getGenericPassword as jest.Mock).mockResolvedValueOnce(false);
    await expect(secureTokenStore.getTokens()).resolves.toBeNull();
  });

  it('getTokens returns null on a malformed entry instead of throwing', async () => {
    (Keychain.getGenericPassword as jest.Mock).mockResolvedValueOnce({
      username: 'risevpn-auth',
      password: 'not-json{',
      service: 'risevpn.auth',
    });
    await expect(secureTokenStore.getTokens()).resolves.toBeNull();
  });

  it('clearTokens resets the entry under the same service', async () => {
    await secureTokenStore.clearTokens();
    expect(Keychain.resetGenericPassword).toHaveBeenCalledWith({
      service: 'risevpn.auth',
    });
  });
});
