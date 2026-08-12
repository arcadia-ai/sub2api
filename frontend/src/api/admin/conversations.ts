import { apiClient } from '../client'
import type { PaginatedResponse } from '@/types'

export interface ConversationMessage {
  role: string
  text: string
  type?: string
  name?: string
}

export interface ConversationSession {
  id: number
  session_uuid: string
  user_id: number
  user_email: string
  api_key_id?: number
  api_key_name: string
  title: string
  first_model: string
  last_model: string
  merge_source: 'isolated' | 'history'
  request_count: number
  total_input_tokens: number
  total_output_tokens: number
  first_request_at: string
  last_request_at: string
  last_status: string
  last_input_tokens: number
  last_output_tokens: number
}

export interface ConversationRequest {
  id: number
  request_uuid: string
  session_id: number
  parent_request_id?: number
  provider: string
  endpoint: string
  requested_model: string
  stream: boolean
  status: string
  http_status: number
  input_tokens: number
  output_tokens: number
  duration_ms: number
  request_truncated: boolean
  response_truncated: boolean
  started_at: string
  completed_at: string
  messages: ConversationMessage[]
}

export interface ConversationDetail {
  session: ConversationSession
  requests: ConversationRequest[]
}

export interface ConversationQuery {
  page?: number
  page_size?: number
  q?: string
  user_id?: number
  api_key_id?: number
  model?: string
  status?: string
  start_time?: string
  end_time?: string
}

export async function list(params: ConversationQuery): Promise<PaginatedResponse<ConversationSession>> {
  const { data } = await apiClient.get('/admin/conversations', { params })
  return data
}

export async function get(id: number): Promise<ConversationDetail> {
  const { data } = await apiClient.get(`/admin/conversations/${id}`)
  return data
}

export async function remove(id: number): Promise<{ deleted: boolean }> {
  const { data } = await apiClient.delete(`/admin/conversations/${id}`)
  return data
}

export async function downloadRaw(requestId: number, kind: 'request' | 'response'): Promise<Blob> {
  const { data } = await apiClient.get(`/admin/conversations/requests/${requestId}/raw-${kind}`, {
    responseType: 'blob'
  })
  return data
}

export const conversationsAPI = { list, get, remove, downloadRaw }
export default conversationsAPI
