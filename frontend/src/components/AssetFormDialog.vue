<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { api } from '../api';
import { assetDepartmentLabel } from '../assetWorkspace';
import type { Asset } from '../types';
import type { Company, Employee } from '../directory';
import { emptyCatalogue } from '../catalogue';
import type { CardField } from '../catalogue';
import EmployeePicker from './EmployeePicker.vue';
import AppIcon from './AppIcon.vue';

const props=defineProps<{ asset?: Asset | null }>();
const emit=defineEmits<{ close: []; saved:[asset:Asset]; directory:[]; settings:[] }>();
const dialog=ref<HTMLDialogElement|null>(null);const loading=ref(true);const busy=ref(false);const error=ref('');
const catalogue=ref(emptyCatalogue());const employees=ref<Employee[]>([]);const companies=ref<Pick<Company,'id'|'name'>[]>([]);const locations=ref<string[]>([]);
const form=reactive<Record<string,any>>({name:'',category_id:null,company_id:null,code_mode:'auto',asset_code:'',location:'',assignee_employee_id:null,received_at:''});
const values=reactive<Record<string,string|number|null>>({});const custom=reactive<Record<string,string|number|null>>({});
const requestId=crypto.randomUUID();const codePreview=ref('');const previewBusy=ref(false);let sequence=0;let disposed=false;
const editing=computed(()=>!!props.asset);const category=computed(()=>catalogue.value.categories.find(c=>c.id===form.category_id));
const card=computed(()=>catalogue.value.cards.find(c=>c.id===category.value?.card_type_id));
const company=computed(()=>companies.value.find(c=>c.id===form.company_id)?.name||'');const employee=computed(()=>employees.value.find(e=>e.id===form.assignee_employee_id));
const categories=computed(()=>catalogue.value.categories.filter(c=>c.active===1 && (!c.parent_id||catalogue.value.categories.some(p=>p.id===c.parent_id&&p.active===1)) && (c.id===props.asset?.category_id||!catalogue.value.categories.some(s=>s.parent_id===c.id))));
const fields=computed(()=>card.value?.fields||[]);
function fieldValue(field:CardField) {return field.key.startsWith('custom_')?custom:values}
watch(()=>category.value?.code,async(prefix)=>{const current=++sequence;codePreview.value='';if(!prefix||editing.value)return;previewBusy.value=true;try{const r=await api.previewAssetCode(prefix);if(current===sequence&&!disposed)codePreview.value=r.code}catch(e){if(current===sequence&&!disposed)codePreview.value='该分类尚未设置编码规则，请联系管理员'}finally{if(current===sequence&&!disposed)previewBusy.value=false}});
watch(fields,(current)=>{for(const field of current){const store=fieldValue(field);if(store[field.key]===undefined)store[field.key]=field.type==='number'?null:''}},{immediate:true});
function close(event?:Event){if(busy.value){event?.preventDefault();return};emit('close')}
async function load(){loading.value=true;error.value='';try{const results=await Promise.all([api.catalogue(),api.listEmployees(),api.assetOptions(''),api.assetCompanies()]);if(disposed)return;catalogue.value=results[0];employees.value=results[1].employees;locations.value=results[2].locations.map(l=>l.name).filter(Boolean);companies.value=results[3].companies;
 if(props.asset){if(props.asset.company&&!companies.value.some(c=>c.name===props.asset?.company))companies.value.push({id:-1,name:props.asset.company});Object.assign(form,{name:props.asset.name,category_id:props.asset.category_id||catalogue.value.categories.find(c=>c.path===props.asset?.category_path)?.id||null,company_id:companies.value.find(c=>c.name===props.asset?.company)?.id||null,asset_code:props.asset.asset_code,location:props.asset.location,assignee_employee_id:props.asset.assignee_employee_id,received_at:props.asset.received_at});for(const field of catalogue.value.cards.flatMap(c=>c.fields)){if(!field.key.startsWith('custom_'))values[field.key]=(props.asset as any)[field.key]??''}Object.assign(custom,props.asset.custom_values||{})}
 }catch(e){if(!disposed)error.value=e instanceof Error?e.message:'资料加载失败'}finally{if(!disposed)loading.value=false}}
async function save(){if(busy.value||loading.value||!card.value||!form.category_id)return;busy.value=true;error.value='';try{
 const body:Record<string,unknown>={name:String(form.name).trim(),category_id:form.category_id,custom_values:{}};
 for(const field of fields.value){const v=fieldValue(field)[field.key];if(field.key.startsWith('custom_'))(body.custom_values as Record<string,unknown>)[field.key]=field.type==='number'&&v!==''&&v!==null?Number(v):v;else body[field.key]=field.type==='number'?Number(v||0):v??''}
 if(editing.value&&props.asset){for(const key of ['category_level1','category_path','company'] as const)body[key]=key==='company'?company.value:props.asset[key];const changed=Object.fromEntries(Object.entries(body).filter(([key,v])=>key==='category_id'||key==='custom_values'||String(v)!==String((props.asset as any)[key]??'')));changed.expectedVersion=props.asset.version;const result=await api.updateAsset(props.asset.asset_code,changed as any);emit('saved',result.asset)}
 else {Object.assign(body,form,{requestId,company_id:form.company_id||null,assignee_employee_id:form.assignee_employee_id||null});const result=await api.createAsset(body);emit('saved',result.asset)}
 }catch(e){error.value=e instanceof Error?e.message:'保存失败，请重试'}finally{busy.value=false}}
onMounted(async()=>{await nextTick();dialog.value?.showModal();void load()});onBeforeUnmount(()=>{disposed=true;sequence++;dialog.value?.close()});
</script>
<template>
 <dialog ref="dialog" class="asset-form-dialog" aria-labelledby="asset-form-title" @cancel="close"><form @submit.prevent="save">
  <header class="catalogue-dialog-heading"><div><span class="eyebrow">{{ editing?'编辑资料':'资产入库' }}</span><h2 id="asset-form-title">{{ editing?'编辑资产资料':'新增资产' }}</h2></div><button type="button" class="icon-button" aria-label="关闭资产表单" :disabled="busy" @click="close()"><AppIcon name="close" /></button></header>
  <div class="asset-form-scroll"><p v-if="loading" class="empty-history" role="status">正在加载分类、卡片和人员…</p><p v-if="error" class="scan-error" role="alert">{{ error }}<button v-if="!catalogue.categories.length" type="button" class="text-button" @click="load">重试</button></p>
  <template v-if="!loading&&catalogue.categories.length"><section class="asset-form-section"><div class="asset-form-section-heading"><h3>基本信息</h3><span>{{ card?.name||'请选择资产分类' }}</span></div><div class="form-grid">
   <label class="field"><span>资产分类 *</span><select v-model="form.category_id" required :disabled="busy"><option :value="null" disabled>请选择分类</option><option v-for="c in categories" :key="c.id" :value="c.id">{{ c.path }}（{{ c.code }}）</option></select></label>
   <label class="field"><span>资产名称 *</span><input v-model="form.name" maxlength="200" required :disabled="busy" placeholder="例如：测试手机" /></label>
   <div class="field"><span>资产编码（自动生成）</span><input v-if="editing" :value="form.asset_code" disabled /><div v-else class="code-preview" role="status">{{ previewBusy?'正在计算…':codePreview||'选择分类后自动生成' }}</div><small v-if="!editing">按分类编码前缀依次生成，保存时分配编号。</small></div>
   <label class="field"><span>所属公司</span><select v-model="form.company_id" :disabled="busy"><option :value="null">暂不设置</option><option v-for="c in companies" :key="c.id" :value="c.id">{{ c.name }}</option></select></label>
   <label class="field"><span>所在位置</span><input v-model="form.location" list="asset-location-options" maxlength="200" :disabled="editing||busy" /><datalist id="asset-location-options"><option v-for="l in locations" :key="l" :value="l" /></datalist><small v-if="editing">已有资产的位置变更请发起调拨申请。</small></label>
  </div></section>
  <section v-if="fields.length" class="asset-form-section"><div class="asset-form-section-heading"><h3>卡片信息</h3><span>按所选分类显示字段</span></div><div class="form-grid"><label v-for="field in fields" :key="`${card?.id}-${field.key}`" class="field" :class="{'field-wide':field.key==='remark'}"><span>{{ field.label }}{{ field.required?' *':'' }}</span><select v-if="field.type==='select'" v-model="fieldValue(field)[field.key]" :required="field.required" :disabled="busy"><option value="">请选择</option><option v-for="option in field.options" :key="option" :value="option">{{ option }}</option></select><textarea v-else-if="field.key==='remark'" v-model="fieldValue(field)[field.key]" rows="3" maxlength="2000" :required="field.required" :disabled="busy" /><input v-else v-model="fieldValue(field)[field.key]" :type="field.type==='text'?'text':field.type" :step="field.type==='number'?'any':undefined" :min="field.type==='number'&&!field.key.startsWith('custom_')?0:undefined" :required="field.required" :disabled="busy" :maxlength="field.key==='photo_url'?2000:200" :placeholder="field.key==='photo_url'?'https://…（照片链接）':undefined" /></label></div></section>
  <section class="asset-form-section"><div class="asset-form-section-heading"><h3>使用信息</h3><span>{{ editing?'使用人变更通过交接审批':'可先入库，之后再领用' }}</span></div><p v-if="!editing&&!employees.length" class="directory-notice">尚未同步公司通讯录。可以先以空闲状态入库，同步人员后再领用。<button type="button" class="text-button" @click="emit('directory')">查看组织与人员</button></p><div class="form-grid"><div class="field"><span>使用人</span><EmployeePicker v-if="!editing" v-model="form.assignee_employee_id" :employees="employees" :company="company" :disabled="busy||!employees.length" /><input v-else :value="asset?.assignee||'未分配'" disabled /></div><label class="field"><span>使用部门</span><input :value="editing&&asset?assetDepartmentLabel(asset):employee?assetDepartmentLabel(employee):''" disabled placeholder="选择人员后自动带出" /></label><label class="field"><span>领用日期</span><input v-model="form.received_at" type="date" :disabled="editing||busy||!form.assignee_employee_id" /></label></div></section></template></div>
  <footer class="asset-form-footer"><p>{{ editing?'保存资料会保留操作记录。':'有使用人时以在用状态入库，未分配时为空闲。' }}</p><div class="form-actions"><button type="button" class="secondary-button" :disabled="busy" @click="close()">取消</button><button type="submit" class="primary-button" :disabled="loading||busy||!card">{{ busy?'保存中…':editing?'保存资料':'保存入库' }}</button></div></footer>
 </form></dialog>
</template>
