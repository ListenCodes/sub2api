<template>
  <BaseDialog :show="show" title="异常阈值" width="narrow" :close-on-click-outside="false" @close="$emit('close')">
    <label for="threshold-success-rate" class="input-label">最低成功率（%）</label><input id="threshold-success-rate" v-model.number="successRate" data-testid="threshold-success-rate" type="number" min="0.1" max="100" step="0.1" class="input w-full" />
    <p v-if="error" class="mt-3 text-sm text-red-600 dark:text-red-400" role="alert">{{ error }}</p>
    <template #footer><button type="button" class="btn btn-secondary" @click="$emit('close')">取消</button><button type="button" class="btn btn-primary" data-testid="threshold-save" :disabled="saving" @click="save">{{ saving ? '保存中' : '保存' }}</button></template>
  </BaseDialog>
</template>
<script setup lang="ts">
import { ref, watch } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { accountMonitorAPI } from '@/api/admin/accountMonitor'
const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const successRate = ref(90)
const saving = ref(false)
const error = ref('')
const message = (value: unknown) => value instanceof Error ? value.message : typeof value === 'object' && value && 'message' in value ? String(value.message) : '保存失败'
async function load() { if (!props.show) return; error.value = ''; try { const value = await accountMonitorAPI.getThreshold(); successRate.value = Number((value.success_rate * 100).toFixed(1)) } catch (value) { error.value = message(value) } }
async function save() { saving.value = true; error.value = ''; try { await accountMonitorAPI.updateThreshold({ scope: 'global', scope_id: 0, success_rate: Number(successRate.value) / 100 }); emit('saved'); emit('close') } catch (value) { error.value = message(value) } finally { saving.value = false } }
watch(() => props.show, load, { immediate: true })
</script>
