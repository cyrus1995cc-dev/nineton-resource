type Controls = { stop: () => void };
type Reader = { decodeFromStream: (stream: MediaStream, video: HTMLVideoElement, callback: (result: { getText: () => string } | undefined, error: unknown, controls: Controls) => void) => Promise<Controls> };

export function createQrCameraSession(options: {
  loadReader: () => Promise<Reader>;
  acquireStream: () => Promise<MediaStream>;
  onResult: (raw: string) => void;
}) {
  type Run = { video: HTMLVideoElement; stream?: MediaStream; controls?: Controls; delivered: boolean };
  let current: Run | null = null;
  function release(run: Run) {
    try { run.controls?.stop(); } finally {
      run.stream?.getTracks().forEach((track) => track.stop());
      if (run.stream && run.video.srcObject === run.stream) run.video.srcObject = null;
    }
  }
  function stop() {
    const run = current; current = null;
    if (run) release(run);
  }
  async function start(video: HTMLVideoElement) {
    stop();
    const run: Run = { video, delivered: false }; current = run;
    try {
      const reader = await options.loadReader();
      if (current !== run) return;
      run.stream = await options.acquireStream();
      if (current !== run) { release(run); return; }
      const controls = await reader.decodeFromStream(run.stream, video, (result, _error, callbackControls) => {
        if (!result || current !== run || run.delivered) return;
        run.delivered = true; run.controls = callbackControls;
        const raw = result.getText(); stop(); options.onResult(raw);
      });
      run.controls = controls;
      if (current !== run) release(run);
    } catch (error) {
      const wasCurrent = current === run;
      if (wasCurrent) current = null;
      release(run);
      if (wasCurrent) throw error;
    }
  }
  return { start, stop };
}
