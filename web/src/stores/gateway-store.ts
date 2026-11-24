import { create } from 'zustand'
import type { Server, ToolProfile, ToolAnnotation, InitMessage } from '@/lib/types'

interface GatewayState {
  servers: Server[]
  tools: ToolProfile[]
  toolAnnotations: Record<string, ToolAnnotation>
  version: string
  defaultAction: string
  mcpHeaderMode: string
  tracingEnabled: boolean
  tracingEndpoint: string
  metricsEnabled: boolean
  authMode: string
  clusterMode: boolean
  user: string
  connected: boolean
  approvalSecretConfigured: boolean
  slackSecretConfigured: boolean
  allowedOrigins: string[]
  approvalTtl: string
  approvalStorage: string
  dashboardAuth: boolean
  authIssuer: string
  authAudience: string
  authJwksUrl: string
  maxDelegationDepth: number
  setInit: (data: InitMessage) => void
  setConnected: (v: boolean) => void
}

export const useGatewayStore = create<GatewayState>((set) => ({
  servers: [],
  tools: [],
  toolAnnotations: {},
  version: '',
  defaultAction: 'allow',
  mcpHeaderMode: 'passthrough',
  tracingEnabled: false,
  tracingEndpoint: '',
  metricsEnabled: false,
  authMode: '',
  clusterMode: false,
  user: '',
  connected: false,
  approvalSecretConfigured: false,
  slackSecretConfigured: false,
  allowedOrigins: [],
  approvalTtl: '',
  approvalStorage: '',
  dashboardAuth: false,
  authIssuer: '',
  authAudience: '',
  authJwksUrl: '',
  maxDelegationDepth: 0,
  setInit: (data) =>
    set({
      servers: data.servers || [],
      tools: data.tools || [],
      toolAnnotations: data.toolAnnotations || {},
      version: data.version || '',
      defaultAction: data.defaultAction || 'allow',
      mcpHeaderMode: data.mcpHeaderMode || 'passthrough',
      tracingEnabled: data.tracingEnabled ?? false,
      tracingEndpoint: data.tracingEndpoint || '',
      metricsEnabled: data.metricsEnabled ?? false,
      authMode: data.authMode || '',
      clusterMode: data.clusterMode ?? false,
      user: data.user || '',
      connected: true,
      approvalSecretConfigured: data.approvalSecretConfigured ?? false,
      slackSecretConfigured: data.slackSecretConfigured ?? false,
      allowedOrigins: data.allowedOrigins || [],
      approvalTtl: data.approvalTtl || '',
      approvalStorage: data.approvalStorage || '',
      dashboardAuth: data.dashboardAuth ?? false,
      authIssuer: data.authIssuer || '',
      authAudience: data.authAudience || '',
      authJwksUrl: data.authJwksUrl || '',
      maxDelegationDepth: data.maxDelegationDepth || 0,
    }),
  setConnected: (v) => set({ connected: v }),
}))
