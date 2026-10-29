<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { api } from '../api';
import { labelMarkup, labelPrintDocument, labelPublicOrigin, labelURL, overflowingLabels, type LabelAsset, type LabelTemplate } from '../assetLabels';
import AppIcon from './AppIcon.vue';
const props = defineProps<{ codes: string[]; canEdit?: boolean }>();
const emit = defineEmits<{ close: []; settings: [] }>();
const dialog = ref<HTMLDialogElement | null>(null); const preview = ref<HTMLElement | null>(null);
const templates = ref<LabelTemplate[]>([]); const assets = ref<LabelAsset[]>([]); const templateId = ref('');
const loading = ref(true); const printing = ref(false); const error = ref(''); const overflowCount = ref(0); const checking = ref(false); const copies = ref(1);
const calibration = reactive({ x: 0, y: 0 }); const origin = labelPublicOrigin;
const template = computed(() => templates.value.find(t => t.id===templateId.value));
const total = computed(() => assets.value.length * copies.value);
const pages = computed(() => { try { return template.value ? assets.value.map(asset => ({ code:asset.asset_code, url:labelURL(asset.asset_code,origin), html:labelMarkup(template.value!,asset,origin,calibration) })) : []; } catch { return []; } });
const problem = computed(() => !Number.isInteger(copies.value) || copies.value < 1 || copies.value > 20 || total.value > 1000 ? '每件1至20张，一批最多1000张' : !Number.isFinite(calibration.x) || !Number.isFinite(calibration.y) || Math.abs(calibration.x)>3 || Math.abs(calibration.y)>3 ? '打印偏移应在 -3 至 3 mm 之间' : !pages.value.length && !loading.value ? '标签内容无效，请检查模板及打印偏移' : overflowCount.value ? `${overflowCount.value} 件资产的文字或二维码超出纸张。请减小字号、减少字段或调整偏移后再打印。` : '');
let disposed = false; let checkSequence = 0;
async function load() {
  loading.value = true; error.value = '';
  try { const [t,a] = await Promise.all([api.labelTemplates(),api.labelAssets(props.codes)]); if (disposed) return; templates.value=t.templates; assets.value=a.assets; templateId.value=t.templates[0]?.id || ''; }
  catch (e) { if(!disposed) error.value=e instanceof Error?e.message:'打印数据加载失败'; }
  finally { if(!disposed) loading.value=false; }
}
function close(event?: Event) { if (printing.value) event?.preventDefault(); else emit('close'); }
async function inspect() { const sequence=++checkSequence;checking.value=true;await nextTick();await document.fonts.ready;if(disposed || sequence!==checkSequence)return;overflowCount.value=preview.value?overflowingLabels(preview.value):0;checking.value=false; }
watch(pages,inspect);
function persistCalibration() { try { localStorage.setItem('asset-label-calibration-v1',JSON.stringify(calibration)); } catch { /* Calibration remains valid for this session. */ } }
watch(calibration,persistCalibration);
async function print() {
  if (printing.value || loading.value || checking.value || error.value || problem.value || !template.value) return;
  const popup = window.open('','_blank');
  if (!popup) { error.value='浏览器拦截了打印窗口，请允许本站弹出窗口后重试'; return; }
  popup.opener=null; printing.value=true;error.value='';
  try {
    const html=labelPrintDocument(template.value,assets.value,copies.value,origin,calibration);
    popup.document.open();popup.document.write(html);popup.document.close();await popup.document.fonts.ready;
    await new Promise<void>(resolve=>popup.requestAnimationFrame(()=>popup.requestAnimationFrame(()=>resolve())));
    if (overflowingLabels(popup.document.body)) throw new Error('打印页内容超出纸张，请调整标签样式后重试');
    popup.focus();popup.print();
  } catch (e) { popup.close();error.value=e instanceof Error?e.message:'无法打开打印预览'; }
  finally { printing.value=false; }
}
onMounted(async()=>{try{const saved=JSON.parse(localStorage.getItem('asset-label-calibration-v1')||'null');if(saved && Number.isFinite(saved.x) && Number.isFinite(saved.y) && Math.abs(saved.x)<=3 && Math.abs(saved.y)<=3)Object.assign(calibration,saved);}catch{}dialog.value?.showModal();await load();});
onBeforeUnmount(()=>{disposed=true;checkSequence++;dialog.value?.close();});
</script>
<template>
  <dialog ref="dialog" class="label-print-dialog label-dialog" aria-labelledby="label-print-title" @cancel="close"><div class="label-print-shell"><header class="label-dialog-header"><div><span class="eyebrow">YLF-280 · 一页一张</span><h2 id="label-print-title">打印资产标签</h2></div><button class="icon-button" :disabled="printing" aria-label="关闭标签打印" @click="close()"><AppIcon name="close" /></button></header><div class="label-dialog-scroll"><p v-if="loading" role="status">正在读取标签样式和资产…</p><p v-if="error" class="scan-error" role="alert">{{ error }} <button v-if="!assets.length" class="text-button" @click="load">重试</button></p><template v-if="assets.length"><div class="label-print-controls"><label class="field label-template-choice"><span>标签样式</span><select v-model="templateId" :disabled="printing"><option v-for="item in templates" :key="item.id" :value="item.id">{{ item.name }}（{{ item.width }} × {{ item.height }} mm）</option></select></label><label class="field"><span>每件打印张数</span><input v-model.number="copies" type="number" min="1" max="20" step="1" :disabled="printing" /></label><label class="field"><span>左右偏移（mm）</span><input v-model.number="calibration.x" type="number" min="-3" max="3" step="0.1" :disabled="printing" /></label><label class="field"><span>上下偏移（mm）</span><input v-model.number="calibration.y" type="number" min="-3" max="3" step="0.1" :disabled="printing" /></label></div><div class="label-print-summary"><strong>{{ assets.length }} 件资产 · 共 {{ total }} 张标签</strong><button v-if="canEdit" class="text-button" :disabled="printing" @click="emit('settings')">管理标签样式</button></div><div class="label-print-guide"><strong>打印设置</strong><p>选择 YLF-280 Label Printer，纸张选择 E {{ template?.width }} × {{ template?.height }} mm，缩放 100%（实际大小）、边距无，关闭页眉页脚。张数已在本页设置，浏览器份数保持 1。</p><p>预览方向应为宽 {{ template?.width }} mm、高 {{ template?.height }} mm。若方向不对，检查驱动中的“右转 90°”。建议先选一件试打，偏移设置仅保存在当前浏览器。</p></div><p class="label-help">二维码直达九吨资产，登录后按现有权限查看。编码完整打印；名称、公司等长文本最多两行，超出显示省略号。</p><p v-if="problem" class="scan-error" role="alert">{{ problem }}</p><div ref="preview" class="label-print-previews"><figure v-for="item in pages" :key="item.code"><div class="label-preview-stage" v-html="item.html"></div><figcaption><strong>{{ item.code }}</strong><a :href="item.url" target="_blank" rel="noopener">查看扫码地址</a></figcaption></figure></div></template></div><footer class="label-dialog-footer"><span>打印预览不会修改资产或原二维码。</span><button class="secondary-button" :disabled="printing" @click="close()">取消</button><button class="primary-button" :disabled="loading || printing || checking || !!problem || !!error || !assets.length" @click="print"><AppIcon name="printer" :size="17" />{{ printing ? '正在打开预览…' : '打开打印预览' }}</button></footer></div></dialog>
</template>
