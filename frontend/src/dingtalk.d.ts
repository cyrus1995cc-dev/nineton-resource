export {};

declare global {
  interface Window {
    dd?: {
      ready?: (callback: () => void) => void;
      runtime?: { permission?: { requestAuthCode: (options: { corpId: string }, callback?: (result: { code?: string; errCode?: number; errMsg?: string }) => void) => unknown } };
      getAuthCode?: (options: { corpId: string }, callback?: (result: { code?: string; errCode?: number; errMsg?: string }) => unknown) => unknown;
    };
  }
}
