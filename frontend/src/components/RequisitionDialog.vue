<script setup lang="ts">
import { computed,onBeforeUnmount,onMounted,reactive,ref } from 'vue';
import { api } from '../api';
import AppIcon from './AppIcon.vue';
const emit=defineEmits<{close:[];submitted:[id:number]}>();
const dialog=ref<HTMLDialogElement|null>(null),loading=ref(true),busy=ref(false),error=ref('');
const options=ref<Awaited<ReturnType<typeof api.requisitionOptions>>|null>(null);
const form=reactive({categoryId:0,quantity:1,neededDate:'',purpose:'',requestId:crypto.randomUUID()});
const selected=computed(()=>options.value?.categories.find(c=>c.id===form.categoryId));
let disposed=false;
function close(e?:Event){if(busy.value)e?.preventDefault();else emit('close')}
async function load(){loading.value=true;error.value='';try{const data=await api.requisitionOptions();if(!disposed)options.value=data}catch(e){if(!disposed)error.value=e instanceof Error?e.message:'读取申请设置失败'}finally{if(!disposed)loading.value=false}}
async function submit(){if(busy.value)return;busy.value=true;error.value='';try{const result=await api.createRequisition({...form});emit('submitted',result.flow.id)}catch(e){error.value=e instanceof Error?e.message:'提交失败，请重试'}finally{busy.value=false}}
onMounted(()=>{dialog.value?.showModal();void load()});onBeforeUnmount(()=>{disposed=true;dialog.value?.close()});
</script>
<template>
 <dialog ref="dialog" class="requisition-dialog" aria-labelledby="requisition-title" @cancel="close">
  <form @submit.prevent="submit"><header class="catalogue-dialog-heading"><div><span class="eyebrow">员工服务</span><h2 id="requisition-title">申请领用设备</h2></div><button class="icon-button" type="button" :disabled="busy" aria-label="关闭领用申请" @click="close()"><AppIcon name="close"/></button></header>
   <div class="requisition-scroll"><p class="requisition-intro">告诉我们你需要什么设备，具体设备由资产管理员分配。姓名、部门会自动带入申请。</p><p v-if="loading" role="status">正在读取设备分类…</p><p v-if="error" class="scan-error" role="alert">{{error}} <button v-if="!options" class="text-button" type="button" @click="load">重试</button></p>
    <fieldset v-if="options" :disabled="busy" class="catalogue-fieldset"><div class="form-grid"><label class="field field-wide"><span>设备分类 *</span><select v-model.number="form.categoryId" required><option :value="0" disabled>请选择所需设备</option><option v-for="category in options.categories" :key="category.id" :value="category.id">{{category.path}}</option></select></label><label class="field"><span>申请数量 *</span><input v-model.number="form.quantity" type="number" min="1" max="20" step="1" required/></label><label class="field"><span>需求日期 *</span><input v-model="form.neededDate" type="date" required/></label><label class="field field-wide"><span>用途及配置要求 *</span><textarea v-model="form.purpose" rows="4" maxlength="2000" required placeholder="例如：日常办公，需要一台可运行设计软件的笔记本电脑。"/></label></div><div class="requisition-summary"><span>审批人</span><strong>{{options.approver.display_name}} · 资产管理员</strong><p>{{options.dingtalk?'提交后进入钉钉审批，处理进度在“申请与审批”查看。':'提交后由固定资产管理员在系统内处理。'}}</p></div><p v-if="selected" class="label-help">本次申请：{{selected.name}} × {{form.quantity}}。提交申请不会立即变更资产归属。</p></fieldset>
   </div><footer class="form-actions"><button type="button" class="secondary-button" :disabled="busy" @click="close()">取消</button><button class="primary-button" :disabled="loading||busy||!form.categoryId||!options">{{busy?'正在提交…':'提交领用申请'}}</button></footer>
  </form>
 </dialog>
</template>
