import axios from 'axios';
import {useAuthStore} from '../stores/authStore';
import {APP_VERSION} from '../config/version';

// Base API URL — points to the Go Fiber backend behind Cloudflare.
// In development (__DEV__) connects to the local machine; in production uses the live API.
const API_BASE_URL = __DEV__
  ? 'http://192.168.10.175:3000/api/v1'
  : 'https://vpnapi.mydayai.uz:9443/api/v1';

const api = axios.create({
  baseURL: API_BASE_URL,
  timeout: 10000,
  headers: {
    'Content-Type': 'application/json',
    // Server-side version gate — requests without this header (or below the
    // server's MIN_APP_VERSION) are rejected with 426 Upgrade Required.
    'X-App-Version': APP_VERSION,
  },
});

// Attach access token to every request
api.interceptors.request.use((config) => {
  const tokens = useAuthStore.getState().tokens;
  if (tokens?.access_token) {
    config.headers.Authorization = `Bearer ${tokens.access_token}`;
  }
  return config;
});

// Token refresh lock — prevents concurrent 401s from triggering multiple refreshes.
// Only the first 401 triggers a refresh; all others wait for the result.
let isRefreshing = false;
let failedQueue: Array<{
  resolve: (value: unknown) => void;
  reject: (reason: unknown) => void;
}> = [];

function processQueue(error: unknown, token: string | null) {
  failedQueue.forEach(({resolve, reject}) => {
    if (error) {
      reject(error);
    } else {
      resolve(token);
    }
  });
  failedQueue = [];
}

// Auto-refresh expired tokens
api.interceptors.response.use(
  (response) => response,
  async (error) => {
    const originalRequest = error.config;

    if (error.response?.status === 401 && !originalRequest._retry) {
      if (isRefreshing) {
        // Another request is already refreshing — wait for it
        return new Promise((resolve, reject) => {
          failedQueue.push({resolve, reject});
        }).then((token) => {
          originalRequest.headers.Authorization = `Bearer ${token}`;
          return api(originalRequest);
        });
      }

      originalRequest._retry = true;
      isRefreshing = true;

      const tokens = useAuthStore.getState().tokens;
      if (tokens?.refresh_token) {
        try {
          const {data} = await axios.post(`${API_BASE_URL}/auth/refresh`, {
            refresh_token: tokens.refresh_token,
          });

          useAuthStore.getState().updateTokens(data.data);
          const newToken = data.data.access_token;
          originalRequest.headers.Authorization = `Bearer ${newToken}`;

          processQueue(null, newToken);
          return api(originalRequest);
        } catch (refreshError) {
          processQueue(refreshError, null);
          // Logout clears stale tokens, then immediately re-initialize
          // via /auth/guest so the app doesn't get stuck in a logged-out
          // loading state. This covers the tg_restore flow where the
          // server deletes the guest user mid-session — the refresh
          // fails because the session row is gone, but the device row
          // is still bound to the real account (with cleared secret
          // hash from PerformRestore), so /auth/guest returns fresh
          // tokens for the correct user automatically.
          //
          // initialize() is idempotent (guarded by the `initializing`
          // module flag) so concurrent 401s from different requests
          // won't cause multiple guest-login calls.
          const store = useAuthStore.getState();
          await store.logout();
          store.initialize();
          return Promise.reject(refreshError);
        } finally {
          isRefreshing = false;
        }
      } else {
        // No refresh token — unblock waiters and re-init as guest
        isRefreshing = false;
        processQueue(error, null);
        const store = useAuthStore.getState();
        await store.logout();
        store.initialize();
      }
    }

    return Promise.reject(error);
  },
);

export default api;
