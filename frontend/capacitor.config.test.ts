import { describe, expect, it } from 'vitest';
import config from './capacitor.config';

describe('Android wrapper configuration', () => {
  it('uses the Android app id and does not configure iOS', () => {
    expect(config.appId).toBe('com.asset.local');
    expect(config.webDir).toBe('dist');
    expect(config).not.toHaveProperty('ios');
  });
});
