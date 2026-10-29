<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue';
import { api } from '../api';
import { defaultLabelTemplate, labelFields, labelMarkup, labelPublicOrigin, labelTemplateProblem, sampleLabelAsset, type LabelTemplate } from '../assetLabels';
import AppIcon from './AppIcon.vue';
const emit = defineEmits<{ print: [] }>();
const templates = ref<LabelTemplate[]>([]); const busy = ref(false); const error = ref(''); const message = ref('');
const draft = ref<LabelTemplate | null>(null); const editor = ref<HTMLDialogElement | null>(null);
const deletion = ref<LabelTemplate | null>(null); const deleteDialog = ref<HTMLDialogElement | null>(null);
const problem = computed(() => draft.value ? labelTemplateProblem(draft.value) : '');
const preview = computed(() => { try { return draft.value ? labelMarkup(draft.value, sampleLabelAsset, labelPublicOrigin) : ''; } catch { return ''; } });
async function load() { busy.value = true; error.value = ''; try { templates.value = (await api.labelTemplates()).templates; } catch (e) { error.value = e instanceof Error ? e.message : '标签样式加载失败'; } finally { busy.value = false; } }
async function edit(template?: LabelTemplate, copy = false) {
  draft.value = template ? { ...template, fields: [...template.fields], ...(copy ? { id: '', version: 1, name: template.name.slice(0, 55) + ' 副本' } : {}) } : defaultLabelTemplate();
  error.value = ''; await nextTick(); editor.value?.showModal();
}
function close(event?: Event) { if (busy.value) event?.preventDefault(); else { editor.value?.close(); draft.value = null; } }
function toggleField(field: string) { if (!draft.value || ['name', 'asset_code'].includes(field)) return; draft.value.fields = draft.value.fields.includes(field) ? draft.value.fields.filter(f => f !== field) : [...draft.value.fields, field]; }
async function save() {
  if (!draft.value || problem.value || busy.value) return; busy.value = true; error.value = '';
  try { await api.saveLabelTemplate(draft.value); editor.value?.close(); draft.value = null; templates.value = (await api.labelTemplates()).templates; message.value = '标签样式已保存'; }
  catch (e) { error.value = e instanceof Error ? e.message : '保存失败'; } finally { busy.value = false; }
}
async function askDelete(template: LabelTemplate) { deletion.value = template; error.value = ''; await nextTick(); deleteDialog.value?.showModal(); }
function cancelDelete(event?: Event) { if (busy.value) event?.preventDefault(); else { deleteDialog.value?.close(); deletion.value = null; } }
async function remove() {
  if (!deletion.value || busy.value) return; busy.value = true; error.value = '';
  try { await api.deleteLabelTemplate(deletion.value); deleteDialog.value?.close(); deletion.value = null; templates.value = (await api.labelTemplates()).templates; message.value = '标签样式已删除'; }
  catch (e) { error.value = e instanceof Error ? e.message : '删除失败'; } finally { busy.value = false; }
}
onMounted(load);
</script>
<template>
  <article class="panel label-settings-panel">
    <div class="panel-header"><div><span class="eyebrow">YLF-280 · 热敏标签</span><h2>资产标签样式</h2></div><div class="form-actions"><button class="secondary-button" @click="emit('print')">前往资产清单打印</button><button class="primary-button" :disabled="busy || templates.length >= 20" @click="edit()"><AppIcon name="plus" :size="17" />新增标签样式</button></div></div>
    <p class="label-help">默认匹配 E 50 × 30 mm 标签纸。先设置样式，再在资产清单勾选资产，点击“打印标签”。新二维码打开九吨资产，原标签记录保留。</p>
    <p v-if="error" class="scan-error" role="alert">{{ error }}<button v-if="!draft && !deletion" class="text-button" @click="load">重新加载</button></p><p v-if="message" class="directory-message" role="status">{{ message }}</p>
    <p v-if="busy && !templates.length" role="status">正在加载标签样式…</p>
    <div class="label-template-grid"><section v-for="template in templates" :key="template.id" class="label-template-card"><header><div><h3>{{ template.name }}</h3><span>{{ template.width }} × {{ template.height }} mm · {{ template.fields.length }} 个字段</span></div><AppIcon name="printer" :size="22" /></header><div class="label-preview-stage" v-html="labelMarkup(template,sampleLabelAsset,labelPublicOrigin)"></div><footer><button class="secondary-button" :disabled="busy" @click="edit(template)">修改</button><button class="text-button" :disabled="busy" @click="edit(template,true)">复制样式</button><button class="text-button label-delete-button" :disabled="busy || templates.length===1" @click="askDelete(template)">删除</button></footer></section></div>
    <dialog ref="editor" class="label-editor-dialog label-dialog" aria-labelledby="label-editor-title" @cancel="close"><form v-if="draft" @submit.prevent="save"><header class="label-dialog-header"><h2 id="label-editor-title">{{ draft.id ? '修改标签样式' : '新增标签样式' }}</h2><button type="button" class="icon-button" :disabled="busy" aria-label="关闭标签样式" @click="close()"><AppIcon name="close" /></button></header><div class="label-dialog-scroll label-editor-layout"><fieldset :disabled="busy" class="label-fields"><label class="field label-wide"><span>样式名称 *</span><input v-model="draft.name" required maxlength="60" /></label><label class="field"><span>标签宽度（mm）*</span><input v-model.number="draft.width" required type="number" min="40" max="100" step="0.1" /></label><label class="field"><span>标签高度（mm）*</span><input v-model.number="draft.height" required type="number" min="25" max="80" step="0.1" /></label><label class="field"><span>二维码大小（mm）*</span><input v-model.number="draft.qrSize" required type="number" min="16" max="40" step="0.1" /></label><label class="field"><span>内边距（mm）*</span><input v-model.number="draft.padding" required type="number" min="1" max="4" step="0.1" /></label><label class="field"><span>文字字号（pt）*</span><input v-model.number="draft.fontSize" required type="number" min="6" max="12" step="0.5" /></label><label class="field"><span>标签标题</span><input v-model="draft.title" maxlength="20" placeholder="可选，例如九吨资产" /></label><div class="label-wide"><span class="label-field-caption">显示字段（最多4个）</span><div class="label-field-choices"><label v-for="(label,key) in labelFields" :key="key"><input type="checkbox" :checked="draft.fields.includes(key)" :disabled="['name','asset_code'].includes(key) || draft.fields.length>=4 && !draft.fields.includes(key)" @change="toggleField(key)" />{{ label }}</label></div></div></fieldset><section class="label-editor-preview"><h3>样式预览</h3><div v-if="preview" class="label-preview-stage" v-html="preview"></div><p v-if="problem" class="scan-error" role="alert">{{ problem }}</p><p class="label-help">示例数据仅展示排版。打印前会读取所选资产并检查内容是否超出纸张；编码完整显示，其他长文本最多两行。</p></section><p v-if="error" class="scan-error label-wide" role="alert">{{ error }}</p></div><footer class="label-dialog-footer"><button type="button" class="secondary-button" :disabled="busy" @click="close()">取消</button><button class="primary-button" :disabled="busy || !!problem">{{ busy ? '保存中…' : '保存标签样式' }}</button></footer></form></dialog>
    <dialog ref="deleteDialog" class="label-delete-dialog" aria-labelledby="label-delete-title" @cancel="cancelDelete"><h2 id="label-delete-title">删除标签样式</h2><p>确定删除“{{ deletion?.name }}”？已打印的标签仍可扫码。</p><p v-if="error" class="scan-error" role="alert">{{ error }}</p><div class="form-actions"><button class="secondary-button" :disabled="busy" @click="cancelDelete()">取消</button><button class="primary-button" :disabled="busy" @click="remove">确认删除样式</button></div></dialog>
  </article>
</template>
