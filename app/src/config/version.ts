// Single source of truth for the client's app version.
// Must match the version in app/package.json and Android versionName.
// Sent as the X-App-Version header on every API request; the server rejects
// any request whose version is missing or below MIN_APP_VERSION.
export const APP_VERSION = '2.2.0';
