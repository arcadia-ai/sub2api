<template>
  <AppLayout>
    <div class="space-y-5">
      <div class="flex flex-col gap-3 border-b border-gray-200 pb-5 dark:border-dark-700 sm:flex-row sm:items-end sm:justify-between">
        <div class="flex flex-1 flex-wrap gap-3">
          <div class="relative min-w-[240px] flex-1 sm:max-w-md">
            <Icon name="search" size="md" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
            <input v-model.trim="filters.q" class="input pl-10" :placeholder="t('admin.conversations.searchPlaceholder')" @keyup.enter="search" />
          </div>
          <input v-model.trim="filters.model" class="input w-full sm:w-48" :placeholder="t('admin.conversations.modelPlaceholder')" @keyup.enter="search" />
          <select v-model="filters.status" class="input w-full sm:w-40" @change="search">
            <option value="">{{ t('admin.conversations.allStatus') }}</option>
            <option value="success">{{ t('admin.conversations.success') }}</option>
            <option value="failed">{{ t('admin.conversations.failed') }}</option>
          </select>
        </div>
        <div class="flex gap-2">
          <button class="btn btn-primary" :disabled="loading" @click="search">
            <Icon name="search" size="sm" class="mr-1.5" />{{ t('common.search') }}
          </button>
          <button class="btn btn-secondary" :disabled="loading" @click="reset">
            <Icon name="refresh" size="sm" class="mr-1.5" />{{ t('common.reset') }}
          </button>
        </div>
      </div>

      <div class="overflow-hidden rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800">
        <div v-if="loading" class="flex h-48 items-center justify-center">
          <LoadingSpinner />
        </div>
        <div v-else-if="sessions.length === 0" class="flex h-48 flex-col items-center justify-center text-gray-400">
          <Icon name="chat" size="xl" class="mb-3" />
          <span class="text-sm">{{ t('admin.conversations.empty') }}</span>
        </div>
        <div v-else class="overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-200 dark:divide-dark-700">
            <thead class="bg-gray-50 text-left text-xs font-medium uppercase text-gray-500 dark:bg-dark-900 dark:text-gray-400">
              <tr>
                <th class="px-5 py-3">{{ t('admin.conversations.detail') }}</th>
                <th class="px-5 py-3">{{ t('admin.conversations.user') }}</th>
                <th class="px-5 py-3">{{ t('admin.conversations.model') }}</th>
                <th class="px-5 py-3">{{ t('admin.conversations.requests') }}</th>
                <th class="px-5 py-3">{{ t('admin.conversations.tokens') }}</th>
                <th class="px-5 py-3">{{ t('admin.conversations.updatedAt') }}</th>
                <th class="w-24 px-5 py-3"></th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="item in sessions" :key="item.id" class="hover:bg-gray-50 dark:hover:bg-dark-700/50">
                <td class="max-w-sm px-5 py-4">
                  <button class="block w-full text-left" @click="openDetail(item.id)">
                    <span class="block truncate font-medium text-gray-900 dark:text-white" :title="item.title">{{ item.title || `#${item.id}` }}</span>
                    <span class="mt-1 block text-xs text-gray-400">{{ item.merge_source === 'history' ? t('admin.conversations.merged') : t('admin.conversations.isolated') }}</span>
                  </button>
                </td>
                <td class="px-5 py-4 text-sm">
                  <div class="max-w-[220px] truncate text-gray-700 dark:text-gray-200" :title="item.user_email">{{ item.user_email }}</div>
                  <div class="mt-1 max-w-[220px] truncate text-xs text-gray-400">{{ item.api_key_name || `Key #${item.api_key_id || '-'}` }}</div>
                </td>
                <td class="px-5 py-4 font-mono text-xs text-gray-600 dark:text-gray-300">{{ item.last_model || '-' }}</td>
                <td class="px-5 py-4 text-sm text-gray-600 dark:text-gray-300">{{ item.request_count }}</td>
                <td class="whitespace-nowrap px-5 py-4 text-sm text-gray-600 dark:text-gray-300">{{ formatNumber(item.total_input_tokens) }} / {{ formatNumber(item.total_output_tokens) }}</td>
                <td class="whitespace-nowrap px-5 py-4 text-sm text-gray-500 dark:text-gray-400">{{ formatTime(item.last_request_at) }}</td>
                <td class="px-5 py-4 text-right">
                  <button class="icon-button" :title="t('common.view')" @click="openDetail(item.id)"><Icon name="eye" size="sm" /></button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <Pagination v-if="total > 0" :total="total" :page="page" :page-size="pageSize" @update:page="changePage" @update:pageSize="changePageSize" />
      </div>
    </div>

    <BaseDialog :show="detailVisible" :title="t('admin.conversations.detail')" width="wide" @close="detailVisible = false">
      <div v-if="detailLoading" class="flex h-48 items-center justify-center"><LoadingSpinner /></div>
      <div v-else-if="detail" class="space-y-5">
        <div class="grid gap-3 border-b border-gray-200 pb-4 text-sm dark:border-dark-700 sm:grid-cols-3">
          <div><span class="text-gray-400">{{ t('admin.conversations.user') }}</span><div class="mt-1 break-all font-medium text-gray-900 dark:text-white">{{ detail.session.user_email }}</div></div>
          <div><span class="text-gray-400">{{ t('admin.conversations.apiKey') }}</span><div class="mt-1 font-medium text-gray-900 dark:text-white">{{ detail.session.api_key_name || '-' }}</div></div>
          <div><span class="text-gray-400">{{ t('admin.conversations.model') }}</span><div class="mt-1 break-all font-mono text-xs text-gray-900 dark:text-white">{{ detail.session.last_model || '-' }}</div></div>
        </div>

        <div v-for="request in detail.requests" :key="request.id" class="border-b border-gray-100 pb-5 last:border-b-0 dark:border-dark-700">
          <div class="mb-3 flex flex-wrap items-center justify-between gap-2 text-xs">
            <div class="flex flex-wrap items-center gap-2 text-gray-500 dark:text-gray-400">
              <span :class="request.status === 'success' ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'">{{ request.http_status }} {{ request.status }}</span>
              <span>{{ request.duration_ms }} ms</span>
              <span class="font-mono">{{ request.endpoint }}</span>
              <span v-if="request.request_truncated || request.response_truncated" class="text-amber-600 dark:text-amber-400">{{ t('admin.conversations.truncated') }}</span>
            </div>
            <div class="flex gap-2">
              <button class="btn btn-ghost btn-sm" @click="download(request.id, 'request')"><Icon name="download" size="xs" class="mr-1" />{{ t('admin.conversations.rawRequest') }}</button>
              <button class="btn btn-ghost btn-sm" @click="download(request.id, 'response')"><Icon name="download" size="xs" class="mr-1" />{{ t('admin.conversations.rawResponse') }}</button>
            </div>
          </div>
          <div v-if="request.messages.length" class="space-y-3">
            <div v-for="(message, index) in request.messages" :key="index" class="flex" :class="message.role === 'assistant' ? 'justify-start' : 'justify-end'">
              <div class="max-w-[88%] rounded-lg px-4 py-3 text-sm leading-6" :class="message.role === 'assistant' ? 'bg-gray-100 text-gray-800 dark:bg-dark-700 dark:text-gray-100' : 'bg-primary-600 text-white'">
                <div class="mb-1 text-[11px] font-medium uppercase opacity-60">{{ message.role }}<span v-if="message.type"> · {{ message.type }}</span></div>
                <div
                  class="conversation-markdown break-words"
                  :class="message.role === 'assistant' ? 'conversation-markdown-assistant' : 'conversation-markdown-user'"
                  v-html="renderMarkdown(message.text)"
                ></div>
              </div>
            </div>
          </div>
          <div v-else class="py-4 text-center text-sm text-gray-400">{{ t('admin.conversations.noText') }}</div>
        </div>
      </div>
      <template #footer>
        <div class="flex justify-between">
          <button v-if="detail" class="btn btn-danger" @click="confirmDelete = true"><Icon name="trash" size="sm" class="mr-1.5" />{{ t('common.delete') }}</button>
          <button class="btn btn-secondary" @click="detailVisible = false">{{ t('common.close') }}</button>
        </div>
      </template>
    </BaseDialog>

    <ConfirmDialog :show="confirmDelete" :title="t('admin.conversations.deleteTitle')" :message="t('admin.conversations.deleteMessage')" danger @confirm="removeCurrent" @cancel="confirmDelete = false" />
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Pagination from '@/components/common/Pagination.vue'
import Icon from '@/components/icons/Icon.vue'
import conversationsAPI, { type ConversationDetail, type ConversationSession } from '@/api/admin/conversations'
import { useAppStore } from '@/stores'

const { t, locale } = useI18n()
const appStore = useAppStore()
const loading = ref(false)
const detailLoading = ref(false)
const detailVisible = ref(false)
const confirmDelete = ref(false)
const sessions = ref<ConversationSession[]>([])
const detail = ref<ConversationDetail | null>(null)
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const filters = reactive({ q: '', model: '', status: '' })

marked.setOptions({
  breaks: true,
  gfm: true,
})

function renderMarkdown(content: string) {
  if (!content) return ''
  return DOMPurify.sanitize(marked.parse(content) as string)
}

async function load() {
  loading.value = true
  try {
    const result = await conversationsAPI.list({ page: page.value, page_size: pageSize.value, ...filters })
    sessions.value = result.items
    total.value = result.total
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.conversations.loadFailed'))
  } finally {
    loading.value = false
  }
}

function search() { page.value = 1; load() }
function reset() { filters.q = ''; filters.model = ''; filters.status = ''; search() }
function changePage(value: number) { page.value = value; load() }
function changePageSize(value: number) { pageSize.value = value; page.value = 1; load() }

async function openDetail(id: number) {
  detailVisible.value = true
  detailLoading.value = true
  try {
    detail.value = await conversationsAPI.get(id)
  } catch (error: any) {
    detailVisible.value = false
    appStore.showError(error?.message || t('admin.conversations.loadFailed'))
  } finally {
    detailLoading.value = false
  }
}

async function download(requestId: number, kind: 'request' | 'response') {
  try {
    const blob = await conversationsAPI.downloadRaw(requestId, kind)
    const url = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = `conversation-${requestId}-${kind}.bin`
    anchor.click()
    URL.revokeObjectURL(url)
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.conversations.downloadFailed'))
  }
}

async function removeCurrent() {
  if (!detail.value) return
  try {
    await conversationsAPI.remove(detail.value.session.id)
    confirmDelete.value = false
    detailVisible.value = false
    appStore.showSuccess(t('admin.conversations.deleted'))
    await load()
  } catch (error: any) {
    appStore.showError(error?.message || t('common.unknownError'))
  }
}

function formatTime(value: string) { return new Intl.DateTimeFormat(locale.value, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(value)) }
function formatNumber(value: number) { return new Intl.NumberFormat(locale.value, { notation: 'compact' }).format(value || 0) }

onMounted(load)
</script>

<style scoped>
.conversation-markdown :deep(> :first-child) {
  margin-top: 0;
}

.conversation-markdown :deep(> :last-child) {
  margin-bottom: 0;
}

.conversation-markdown :deep(p),
.conversation-markdown :deep(ul),
.conversation-markdown :deep(ol),
.conversation-markdown :deep(blockquote),
.conversation-markdown :deep(pre),
.conversation-markdown :deep(table) {
  @apply mb-3;
}

.conversation-markdown :deep(h1) { @apply mb-3 mt-4 text-xl font-bold; }
.conversation-markdown :deep(h2) { @apply mb-2 mt-4 text-lg font-bold; }
.conversation-markdown :deep(h3) { @apply mb-2 mt-3 text-base font-semibold; }
.conversation-markdown :deep(h4) { @apply mb-2 mt-3 text-sm font-semibold; }
.conversation-markdown :deep(ul) { @apply list-disc pl-5; }
.conversation-markdown :deep(ol) { @apply list-decimal pl-5; }
.conversation-markdown :deep(li) { @apply my-1 pl-1; }
.conversation-markdown :deep(a) { @apply break-all font-medium underline underline-offset-2; }
.conversation-markdown :deep(blockquote) { @apply border-l-4 py-1 pl-3 italic; }
.conversation-markdown :deep(code) { @apply rounded px-1.5 py-0.5 font-mono text-xs; }
.conversation-markdown :deep(pre) { @apply max-w-full overflow-x-auto rounded-md p-3; }
.conversation-markdown :deep(pre code) { @apply bg-transparent p-0 text-inherit; }
.conversation-markdown :deep(table) { @apply block max-w-full overflow-x-auto border-collapse; }
.conversation-markdown :deep(th),
.conversation-markdown :deep(td) { @apply border px-3 py-2 text-left align-top; }
.conversation-markdown :deep(img) { @apply my-3 max-w-full rounded-md; }
.conversation-markdown :deep(hr) { @apply my-4 border-0 border-t; }

.conversation-markdown-assistant :deep(a) { @apply text-blue-600 dark:text-blue-400; }
.conversation-markdown-assistant :deep(blockquote) { @apply border-gray-300 text-gray-600 dark:border-dark-500 dark:text-gray-300; }
.conversation-markdown-assistant :deep(code) { @apply bg-gray-200 text-pink-700 dark:bg-dark-800 dark:text-pink-300; }
.conversation-markdown-assistant :deep(pre) { @apply bg-gray-900 text-gray-100; }
.conversation-markdown-assistant :deep(th),
.conversation-markdown-assistant :deep(td) { @apply border-gray-300 dark:border-dark-500; }
.conversation-markdown-assistant :deep(hr) { @apply border-gray-300 dark:border-dark-500; }

.conversation-markdown-user :deep(a) { @apply text-white; }
.conversation-markdown-user :deep(blockquote) { @apply border-white/50 text-white/90; }
.conversation-markdown-user :deep(code) { @apply bg-black/20 text-white; }
.conversation-markdown-user :deep(pre) { @apply bg-black/30 text-white; }
.conversation-markdown-user :deep(th),
.conversation-markdown-user :deep(td) { @apply border-white/30; }
.conversation-markdown-user :deep(hr) { @apply border-white/40; }
</style>
