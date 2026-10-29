<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue';
import { api } from '../api';
import type { Company, CompanyInput, DirectoryStatus } from '../directory';
import { descendantIds, treeRows } from '../organization';

const emit = defineEmits<{ changed: []; directory: [] }>();
const companies = ref<Company[]>([]); const directory = ref<DirectoryStatus | null>(null);
const loading = ref(true); const busy = ref(false); const error = ref(''); const formError = ref(''); const message = ref('');
const search = ref(''); const selected = ref(0); const page = ref(1); const editing = ref<Company | null>(null);
const dialog = ref<HTMLDialogElement | null>(null);
const form = reactive<CompanyInput>({ code: '', name: '', parent_id: null, remark: '', dingtalk_department_id: null });
const tree = computed(() => treeRows(companies.value, c => c.id, c => c.parent_id));
const scope = computed(() => selected.value ? descendantIds(companies.value, selected.value, c => c.id, c => c.parent_id) : null);
const filtered = computed(() => companies.value.filter(c => (!scope.value || scope.value.has(c.id)) && `${c.code} ${c.name} ${c.parent_name ?? ''} ${c.remark}`.toLowerCase().includes(search.value.trim().toLowerCase())));
const shown = computed(() => filtered.value.slice((page.value - 1) * 20, page.value * 20));
const excludedParents = computed(() => editing.value ? descendantIds(companies.value, editing.value.id, c => c.id, c => c.parent_id) : new Set<number>());
const departments = computed(() => treeRows(directory.value?.departments.filter(d => !d.missing && d.corp_id === directory.value?.corpId) ?? [], d => d.dingtalk_id, d => d.parent_id));
watch([search, selected], () => { page.value = 1; });
async function load() {
  const [c, d] = await Promise.all([api.listCompanies(), api.directoryStatus()]); companies.value = c.companies; directory.value = d;
  page.value = Math.min(page.value, Math.max(1, Math.ceil(filtered.value.length / 20)));
}
async function refresh() { loading.value = true; error.value = ''; try { await load(); } catch (e) { error.value = e instanceof Error ? e.message : '公司加载失败'; } finally { loading.value = false; } }
async function open(company: Company | null) {
  editing.value = company; formError.value = '';
  Object.assign(form, company ? { code: company.code, name: company.name, parent_id: company.parent_id, remark: company.remark, dingtalk_department_id: company.dingtalk_corp_id === directory.value?.corpId ? company.dingtalk_department_id : null } : { code: '', name: '', parent_id: selected.value || null, remark: '', dingtalk_department_id: null });
  await nextTick(); dialog.value?.showModal();
}
async function save() {
  busy.value = true; formError.value = ''; let saved = false;
  try {
    const input = { ...form, code: form.code.trim(), name: form.name.trim(), remark: form.remark.trim() };
    if (editing.value) await api.updateCompany(editing.value.id, { ...input, expectedVersion: editing.value.version }); else await api.createCompany(input);
    saved = true; dialog.value?.close(); message.value = `${input.name}已${editing.value ? '修改' : '新增'}`; emit('changed'); await load();
  } catch (e) { const text = e instanceof Error ? e.message : '公司保存失败'; if (saved) error.value = `公司已保存，列表刷新失败：${text}`; else formError.value = text; }
  finally { busy.value = false; }
}
onMounted(refresh);
</script>

<template>
  <article class="panel company-panel">
    <div class="panel-header"><div><span class="eyebrow">基础资料</span><h2>公司管理</h2></div><div class="form-actions"><button class="secondary-button" @click="emit('directory')">钉钉组织架构</button><button class="primary-button" :disabled="loading || directory?.syncing" @click="open(null)">新增公司</button></div></div>
    <p v-if="error" class="scan-error" role="alert">{{ error }}</p><p v-if="message" class="directory-message" role="status">{{ message }}</p>
    <p class="organization-help">维护公司及上下级关系。绑定钉钉部门后，该部门及下级部门的人员归属此公司；下级公司绑定优先。</p>
    <p v-if="directory?.syncing" class="directory-notice" role="status">通讯录正在同步，完成后可新增或修改公司。</p>
    <div class="organization-layout">
      <aside class="organization-tree" aria-label="公司层级"><button :class="{ selected: !selected }" @click="selected = 0">全部公司 <small>{{ companies.length }}</small></button><button v-for="row in tree" :key="row.item.id" :class="{ selected: selected === row.item.id }" :style="{ paddingLeft: `${12 + Math.min(row.depth, 6) * 16}px` }" @click="selected = row.item.id"><span>{{ row.item.code }} · {{ row.item.name }}</span></button></aside>
      <div class="organization-main">
        <form class="directory-search" @submit.prevent="page = 1"><label class="field"><span>搜索公司编码、名称或备注</span><input v-model="search" type="search" placeholder="输入关键词搜索" /></label><button class="secondary-button" type="button" :disabled="loading" @click="refresh">刷新</button></form>
        <div class="list-summary" role="status">{{ loading ? '正在加载公司…' : `共 ${filtered.length} 家公司` }}</div>
        <div class="company-table-wrap"><table class="company-table"><thead><tr><th scope="col">公司编码</th><th scope="col">公司名称</th><th scope="col">上级公司</th><th scope="col">钉钉部门</th><th scope="col">备注</th><th scope="col">操作</th></tr></thead><tbody><tr v-for="c in shown" :key="c.id"><td data-label="公司编码">{{ c.code }}</td><td data-label="公司名称"><strong>{{ c.name }}</strong><small>资产 {{ c.assets_count }} 项 · 有效员工 {{ c.employees_count }} 人</small></td><td data-label="上级公司">{{ c.parent_name || '无上级公司' }}</td><td data-label="钉钉部门">{{ c.dingtalk_department_id ? `${directory?.departments.find(d => d.corp_id === c.dingtalk_corp_id && d.dingtalk_id === c.dingtalk_department_id)?.name || '待核对部门'}（${c.dingtalk_department_id}）` : '未绑定' }}</td><td data-label="备注">{{ c.remark || '—' }}</td><td data-label="操作"><button class="text-button" :disabled="directory?.syncing" :aria-label="`修改${c.name}`" @click="open(c)">修改</button></td></tr></tbody></table></div>
        <div v-if="!loading && !shown.length" class="empty-history">{{ search || selected ? '没有符合条件的公司，请调整筛选条件。' : '暂无公司，点击“新增公司”开始维护。' }}</div>
        <div class="pager"><span>显示 {{ shown.length }} 家</span><div><button :disabled="page <= 1" @click="page--">上一页</button><span>{{ page }} / {{ Math.max(1, Math.ceil(filtered.length / 20)) }}</span><button :disabled="page * 20 >= filtered.length" @click="page++">下一页</button></div></div>
      </div>
    </div>
    <dialog ref="dialog" class="company-dialog" aria-labelledby="company-form-title" @cancel="busy && $event.preventDefault()">
      <form @submit.prevent="save"><h2 id="company-form-title">{{ editing ? '修改公司' : '新增公司' }}</h2><p class="organization-help">公司名称变更后，历史资产台账和审批记录保留原名称。</p>
        <fieldset :disabled="busy" class="company-fieldset"><div class="form-grid">
          <label class="field"><span>公司编码 <span aria-hidden="true">*</span></span><input v-model="form.code" required maxlength="80" pattern="[A-Za-z0-9][A-Za-z0-9_.\-]{0,79}" placeholder="例如 0102" autofocus /><small>字母、数字、点、横线或下划线</small></label>
          <label class="field"><span>公司名称 <span aria-hidden="true">*</span></span><input v-model="form.name" required maxlength="200" placeholder="填写完整公司名称" /></label>
          <label class="field"><span>上级公司</span><select v-model="form.parent_id"><option :value="null">无上级公司</option><option v-for="row in tree.filter(r => !excludedParents.has(r.item.id))" :key="row.item.id" :value="row.item.id">{{ '　'.repeat(Math.min(row.depth, 6)) }}{{ row.item.code }} · {{ row.item.name }}</option></select></label>
          <label class="field"><span>绑定钉钉部门</span><select v-model="form.dingtalk_department_id"><option :value="null">暂不绑定</option><option v-if="form.dingtalk_department_id && !departments.some(r => r.item.dingtalk_id === form.dingtalk_department_id)" :value="form.dingtalk_department_id">原绑定部门 {{ form.dingtalk_department_id }}（待核对）</option><option v-for="row in departments" :key="row.item.id" :value="row.item.dingtalk_id">{{ '　'.repeat(Math.min(row.depth, 6)) }}{{ row.item.name }}（{{ row.item.dingtalk_id }}）</option></select><small>{{ directory?.configured ? '先同步通讯录，再选择本公司对应的钉钉部门。' : '请先在服务端配置钉钉应用凭证并同步通讯录。' }}</small></label>
          <label class="field company-wide"><span>备注</span><textarea v-model="form.remark" maxlength="1000" rows="3" /></label>
        </div></fieldset>
        <p v-if="formError" class="scan-error" role="alert">{{ formError }}</p><div class="form-actions company-form-actions"><button type="button" class="secondary-button" :disabled="busy" @click="dialog?.close()">取消</button><button class="primary-button" :disabled="busy">{{ busy ? '保存中…' : '保存公司' }}</button></div>
      </form>
    </dialog>
  </article>
</template>
