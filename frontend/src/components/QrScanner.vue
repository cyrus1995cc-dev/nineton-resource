<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { createQrCameraSession } from '../qrCamera';
const props = defineProps<{ disabled?: boolean }>();
const emit = defineEmits<{ scan: [raw: string] }>();
const active = ref(false); const starting = ref(false); const error = ref('');
const video = ref<HTMLVideoElement | null>(null);
let runId = 0;
const session = createQrCameraSession({
  loadReader: async () => { const { BrowserQRCodeReader } = await import('@zxing/browser'); return new BrowserQRCodeReader(); },
  acquireStream: () => navigator.mediaDevices.getUserMedia({ video: { facingMode: { ideal: 'environment' } }, audio: false }),
  onResult: (raw) => { runId++; active.value = false; starting.value = false; emit('scan', raw); },
});
function stop() { runId++; active.value = false; starting.value = false; session.stop(); }
async function open() {
  if (props.disabled || active.value) return;
  error.value = '';
  if (!navigator.mediaDevices?.getUserMedia) {
    error.value = '当前页面无法使用摄像头。电脑可用 localhost，手机可使用 Android App 或 HTTPS 网页；也可输入二维码原文。'; return;
  }
  const openingId = ++runId;
  active.value = true; starting.value = true; await nextTick();
  if (!active.value || !video.value) return;
  try { await session.start(video.value); }
  catch (err) {
    stop(); const name = err instanceof Error ? err.name : '';
    error.value = name === 'NotAllowedError' ? '摄像头权限未开启，请在应用或浏览器设置中允许使用摄像头。'
      : name === 'NotFoundError' ? '未发现可用摄像头，请输入资产编码。'
      : name === 'NotReadableError' ? '摄像头可能被其他应用占用，请关闭后重试。' : '摄像头启动失败，请重试或输入二维码原文。';
  } finally { if (openingId === runId) starting.value = false; }
}
function onVisibilityChange() { if (document.hidden) stop(); }
watch(() => props.disabled, (value) => { if (value) stop(); });
onMounted(() => document.addEventListener('visibilitychange', onVisibilityChange));
onBeforeUnmount(() => { stop(); document.removeEventListener('visibilitychange', onVisibilityChange); });
</script>
<template>
  <div class="qr-scanner">
    <div v-if="active" class="camera-preview"><video ref="video" class="scan-video" autoplay muted playsinline aria-label="二维码扫描预览"></video><p role="status">{{ starting ? '正在启动摄像头…' : '请将资产二维码放入画面，识别后自动关闭' }}</p></div>
    <button v-if="active" type="button" class="secondary-button full-button" @click="stop">关闭摄像头</button>
    <button v-else type="button" class="secondary-button full-button" :disabled="disabled" @click="open">打开摄像头扫码</button>
    <p v-if="error" class="scan-error" role="alert">{{ error }}</p>
  </div>
</template>
