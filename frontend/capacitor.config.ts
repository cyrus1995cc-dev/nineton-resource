import type { CapacitorConfig } from '@capacitor/cli';

const config: CapacitorConfig = {
  appId: 'com.asset.local',
  appName: '九吨资产',
  webDir: 'dist',
  plugins: {
    Camera: {
      permissions: ['camera'],
    },
  },
};

export default config;
