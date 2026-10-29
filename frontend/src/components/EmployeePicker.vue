<script setup lang="ts">
import { computed, nextTick, ref, useId } from 'vue';
import type { Employee } from '../directory';
import { assetDepartmentLabel } from '../assetWorkspace';
import AppIcon from './AppIcon.vue';

const props = defineProps<{ modelValue: number | null | undefined; employees: Employee[]; disabled?: boolean; required?: boolean; company?: string; label?: string }>();
const emit = defineEmits<{ 'update:modelValue': [id: number | null] }>();
const dialog = ref<HTMLDialogElement | null>(null); const search = ref(''); const companyOnly = ref(false);
const titleId = useId();
const selected = computed(() => props.employees.find(e => e.id === props.modelValue));
const candidates = computed(() => props.employees.filter(e => (!companyOnly.value || !props.company || e.company === props.company || e.memberships?.some(m => m.company_name === props.company)) && `${e.name} ${e.department} ${e.company} ${e.job_number}`.toLowerCase().includes(search.value.trim().toLowerCase())));
const shown = ref(60);
async function open() { if (props.disabled) return; search.value = ''; shown.value=60; companyOnly.value=false; await nextTick(); dialog.value?.showModal(); }
function choose(id: number | null) { emit('update:modelValue', id); dialog.value?.close(); }
</script>
<template>
  <div class="employee-picker">
    <button type="button" class="employee-picker-trigger" :disabled="disabled" :aria-label="label || '选择使用人'" aria-haspopup="dialog" @click="open"><span>{{ selected ? `${selected.name} · ${assetDepartmentLabel(selected) || '未设置部门'}` : modelValue ? '当前使用人（原有记录）' : '请选择公司人员' }}</span><AppIcon name="chevron" :size="16" /></button>
    <input v-if="required && !disabled" class="picker-validation" tabindex="-1" aria-label="已选择人员" :value="modelValue ? String(modelValue) : ''" required @invalid="open" />
    <dialog ref="dialog" class="employee-picker-dialog" :aria-labelledby="titleId"><header class="catalogue-dialog-heading"><div><h2 :id="titleId">选择公司人员</h2><p>从通讯录选择，自动带出使用部门</p></div><button type="button" class="icon-button" aria-label="关闭人员选择" @click="dialog?.close()"><AppIcon name="close" /></button></header><div class="employee-picker-search"><label class="field"><span>搜索姓名、部门或工号</span><input v-model="search" type="search" @input="shown=60" /></label><label v-if="company" class="checkbox-label"><input v-model="companyOnly" type="checkbox" />仅看 {{ company }}</label></div><div class="employee-picker-results"><button v-if="!required" type="button" @click="choose(null)"><span class="person-avatar"><AppIcon name="box" /></span><span><strong>暂不分配</strong><small>以空闲状态入库</small></span></button><button v-for="employee in candidates.slice(0,shown)" :key="employee.id" type="button" :class="{selected:employee.id===modelValue}" @click="choose(employee.id)"><span class="person-avatar">{{ employee.name.slice(0,1) }}</span><span><strong>{{ employee.name }}<small v-if="employee.job_number"> · {{ employee.job_number }}</small></strong><small>{{ assetDepartmentLabel(employee) || '未设置部门' }}<template v-if="employee.company"> · {{ employee.company }}</template></small></span><AppIcon v-if="employee.id===modelValue" name="check" /></button><p v-if="!candidates.length" class="empty-history">{{ employees.length ? '没有匹配人员，请调整搜索条件。' : '尚未同步公司通讯录，请联系管理员完成钉钉配置并同步人员。' }}</p><button v-if="candidates.length>shown" type="button" class="text-button" @click="shown+=60">加载更多（共 {{ candidates.length }} 人）</button></div></dialog>
  </div>
</template>
