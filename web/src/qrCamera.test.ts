import { describe, expect, it, vi } from 'vitest';
import { createQrCameraSession } from './qrCamera';
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>((r) => { resolve = r; }); return { promise, resolve }; }
function media() { const stop = vi.fn(); const stream = { getTracks: () => [{ stop }] } as unknown as MediaStream; return { stream, stop }; }
describe('camera lifecycle', () => {
  it('releases a stream granted after the user closed the scanner', async () => {
    const pending = deferred<MediaStream>(); const { stream, stop } = media();
    const decode = vi.fn(); const onResult = vi.fn();
    const session = createQrCameraSession({ loadReader: async () => ({ decodeFromStream: decode }), acquireStream: () => pending.promise, onResult });
    const startup = session.start({ srcObject: null } as HTMLVideoElement);
    await Promise.resolve(); session.stop(); pending.resolve(stream); await startup;
    expect(stop).toHaveBeenCalled(); expect(decode).not.toHaveBeenCalled(); expect(onResult).not.toHaveBeenCalled();
  });
  it('stops an early decode, emits once, and stops controls returned after cancellation', async () => {
    const { stream, stop } = media(); const controls = { stop: vi.fn() }; const onResult = vi.fn();
    const video = { srcObject: stream } as HTMLVideoElement;
    const session = createQrCameraSession({
      loadReader: async () => ({ decodeFromStream: async (_stream, _video, callback) => {
        callback({ getText: () => 'INV-001' }, undefined, controls);
        callback({ getText: () => 'INV-002' }, undefined, controls);
        return controls;
      } }), acquireStream: async () => stream, onResult,
    });
    await session.start(video);
    expect(onResult).toHaveBeenCalledExactlyOnceWith('INV-001');
    expect(controls.stop).toHaveBeenCalled(); expect(stop).toHaveBeenCalled(); expect(video.srcObject).toBeNull();
  });
  it('cleans up a failed decoder and skips permission requests after cancellation while loading', async () => {
    const { stream, stop } = media();
    const session = createQrCameraSession({ loadReader: async () => ({ decodeFromStream: async () => { throw new Error('decoder failed'); } }), acquireStream: async () => stream, onResult: vi.fn() });
    await expect(session.start({ srcObject: stream } as HTMLVideoElement)).rejects.toThrow('decoder failed');
    expect(stop).toHaveBeenCalled();
    const load = deferred<any>(); const acquire = vi.fn();
    const cancelled = createQrCameraSession({ loadReader: () => load.promise, acquireStream: acquire, onResult: vi.fn() });
    const startup = cancelled.start({ srcObject: null } as HTMLVideoElement); cancelled.stop(); load.resolve({}); await startup;
    expect(acquire).not.toHaveBeenCalled();
  });
});
