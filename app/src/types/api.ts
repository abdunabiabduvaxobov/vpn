// API response types for the Go Fiber backend

export interface ApiResponse<T> {
  data: T;
  error?: string;
}

export interface User {
  id: string;
  email_hash?: string;
  full_name: string;
  subscription_tier: 'free' | 'premium' | 'ultimate';
  subscription_expires_at: string | null;
  created_at: string;
  // Phase 5 — Apple/Google SSO additions (backend Phase 2 D-11 columns).
  auth_provider?: 'guest' | 'apple' | 'google';
  email?: string;
  email_verified?: boolean;
}

export interface AuthTokens {
  access_token: string;
  refresh_token: string;
  expires_in: number; // seconds until access token expires
}

export interface Server {
  id: string;
  hostname: string;
  ip_address: string;
  region: string;
  city: string;
  country: string;
  country_code: string;
  protocol: string;
  load_percent: number;
  is_active: boolean;
}

export interface ServerConfig {
  server_address: string;
  server_port: number;
  protocol: string;
  user_id: string;
  reality?: {
    public_key: string;
    short_id: string;
    server_name: string;
    fingerprint: string;
  };
  websocket?: {
    host: string;
    path: string;
  };
  awg?: {
    public_key: string;
    endpoint: string;
    allowed_ips: string;
    jc: number;
    jmin: number;
    jmax: number;
    s1: number;
    s2: number;
    h1: number;
    h2: number;
    h3: number;
    h4: number;
  };
  protocol_priority?: string[];
}

export interface Subscription {
  id?: string;
  plan: 'free' | 'premium' | 'ultimate';
  is_active: boolean;
  started_at?: string;
  expires_at?: string | null;
  max_devices: number;
}

export interface BoundDevice {
  id: string;
  user_id: string;
  device_id: string;
  platform: string;
  model: string;
  first_seen_at: string;
  last_seen_at: string;
}
