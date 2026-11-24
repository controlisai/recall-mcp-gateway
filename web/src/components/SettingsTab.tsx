import { useState, useEffect, useCallback } from 'react'
import { useGatewayStore } from '@/stores/gateway-store'
import {
  saveRuntimeConfig,
  saveWebhookConfig,
  testWebhook,
  fetchAuditInfo,
  saveAuditConfig,
  purgeAudit,
  connectPlatform,
  syncToPlatform,
  type AuditInfo,
} from '@/lib/api'

const MONO = "'JetBrains Mono', 'Cascadia Code', 'SF Mono', 'Fira Code', monospace"

const sectionTitle: React.CSSProperties = {
  fontSize: '14px', fontWeight: 600, color: 'var(--text)',
  marginBottom: '12px',
}

const fieldLabel: React.CSSProperties = {
  fontSize: '11px', fontWeight: 600, color: 'var(--muted)',
  textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: '4px', display: 'block',
}

const selectStyle: React.CSSProperties = {
  background: 'var(--bg)', border: '1px solid var(--border)', borderRadius: 'var(--radius)',
  color: 'var(--text)', padding: '6px 10px', fontSize: '13px', width: '100%', outline: 'none',
}

const inputStyle: React.CSSProperties = {
  ...selectStyle, fontFamily: MONO,
}

const readOnly: React.CSSProperties = {
  fontSize: '13px', color: 'var(--text)', padding: '6px 0',
}

const badge = (text: string, color: string) => (
  <span style={{
    fontSize: '11px', padding: '2px 7px', borderRadius: '3px',
    background: `color-mix(in srgb, ${color} 12%, transparent)`, color,
    fontWeight: 600,
  }}>
    {text}
  </span>
)

export function SettingsTab() {
  const gw = useGatewayStore()
  const [auditInfo, setAuditInfo] = useState<AuditInfo | null>(null)
  const [maxEntries, setMaxEntries] = useState('')
  const [approvalWebhook, setApprovalWebhook] = useState('')
  const [alertWebhook, setAlertWebhook] = useState('')
  const [webhookStatus, setWebhookStatus] = useState<string | null>(null)
  const [alertWebhookStatus, setAlertWebhookStatus] = useState<string | null>(null)
  const [savedField, setSavedField] = useState<string | null>(null)

  // Platform connection
  const [platformConnected, setPlatformConnected] = useState(false)
  const [connectUrl, setConnectUrl] = useState('')
  const [connectToken, setConnectToken] = useState('')
  const [connectTenantId, setConnectTenantId] = useState('')
  const [connectConsoleUrl, setConnectConsoleUrl] = useState('')
  const [connectEngineUrl, setConnectEngineUrl] = useState('')
  const [connectApiKey, setConnectApiKey] = useState('')
  const [connectOrgId, setConnectOrgId] = useState('')
  const [connectError, setConnectError] = useState<string | null>(null)
  const [connectResult, setConnectResult] = useState<{ engineConnected?: boolean; engineWarning?: string; products?: Array<{ id: string; name: string }>; consoleUrl?: string } | null>(null)
  const [syncProductId, setSyncProductId] = useState('')
  const [syncResult, setSyncResult] = useState<string | null>(null)
  const [syncing, setSyncing] = useState(false)

  const loadAudit = useCallback(async () => {
    try {
      const info = await fetchAuditInfo()
      setAuditInfo(info)
      if (info.maxEntries != null) setMaxEntries(String(info.maxEntries))
    } catch { /* ignore */ }
  }, [])

  useEffect(() => { loadAudit() }, [loadAudit])

  const handleRuntimeSave = async (key: string, value: string) => {
    try {
      await saveRuntimeConfig({ [key]: value })
      // Update Zustand store so UI reflects the change immediately
      useGatewayStore.setState({ [key]: value } as Record<string, unknown>)
      setSavedField(key)
      setTimeout(() => setSavedField(null), 2000)
    } catch (e) {
      console.error(e)
    }
  }

  const handleSaveApprovalWebhook = async () => {
    try {
      await saveWebhookConfig({ approvalWebhookUrl: approvalWebhook })
      setWebhookStatus('Saved')
      setTimeout(() => setWebhookStatus(null), 2000)
    } catch { setWebhookStatus('Error') }
  }

  const handleTestWebhook = async () => {
    if (!approvalWebhook) return
    try {
      await testWebhook(approvalWebhook)
      setWebhookStatus('✓ Test delivered')
      setTimeout(() => setWebhookStatus(null), 3000)
    } catch { setWebhookStatus('✕ Test failed'); setTimeout(() => setWebhookStatus(null), 3000) }
  }

  const handleSaveAlertWebhook = async () => {
    try {
      await saveWebhookConfig({ alertWebhookUrl: alertWebhook })
      setAlertWebhookStatus('Saved')
      setTimeout(() => setAlertWebhookStatus(null), 2000)
    } catch { setAlertWebhookStatus('Error') }
  }

  const handleSaveAuditMax = async () => {
    const val = parseInt(maxEntries, 10)
    if (isNaN(val)) return
    try {
      await saveAuditConfig(val)
      await loadAudit()
    } catch (e) { console.error(e) }
  }

  const handlePurge = async () => {
    if (!confirm('Delete all audit entries? This cannot be undone.')) return
    try {
      await purgeAudit()
      // Clear feed store so Discover tab doesn't show stale entries
      const { useFeedStore } = await import('@/stores/feed-store')
      useFeedStore.getState().loadRecent([])
      await loadAudit()
    } catch (e) { console.error(e) }
  }

  const handleConnect = async () => {
    if (!connectUrl.trim()) { setConnectError('Console API URL is required'); return }
    try {
      setConnectError(null)
      const resp = await connectPlatform({
        url: connectUrl, token: connectToken, tenantId: connectTenantId,
        consoleTenantUrl: connectConsoleUrl, engineUrl: connectEngineUrl,
        engineApiKey: connectApiKey, defaultOrgId: connectOrgId,
      })
      if (resp.error) { setConnectError(resp.error); return }
      setPlatformConnected(true)
      setConnectResult(resp)
    } catch (e) { setConnectError(String(e)) }
  }

  const handleSync = async () => {
    if (!syncProductId) return
    try {
      setSyncing(true); setSyncResult(null)
      const resp = await syncToPlatform(syncProductId)
      setSyncResult(resp.status === 'complete' ? 'Sync complete' : `Sync ${resp.status}`)
    } catch (e) { setSyncResult(`Error: ${e}`) }
    finally { setSyncing(false) }
  }

  const proxyEndpoint = `${window.location.protocol}//${window.location.host}/mcp`

  return (
    <div style={{ display: 'flex', height: '100%', overflow: 'hidden' }}>
      {/* Left: Configuration */}
      <div style={{ flex: 1, overflow: 'auto', padding: '20px', borderRight: '1px solid var(--border)' }}>

        {/* Gateway Mode */}
        <div style={{ marginBottom: '24px' }}>
          <div style={{ ...sectionTitle, display: 'flex', alignItems: 'center', gap: '8px' }}>
            Gateway Mode
            {badge(gw.clusterMode ? 'Cluster (Redis)' : 'Standalone', gw.clusterMode ? 'var(--accent)' : 'var(--muted)')}
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: '16px' }}>
            <div>
              <label style={fieldLabel}>Default Action</label>
              <div style={readOnly}>
                {badge(gw.defaultAction || 'allow', gw.defaultAction === 'block' ? 'var(--red)' : 'var(--green)')}
                <span style={{ fontSize: '11px', color: 'var(--muted)', marginLeft: '6px' }}>via Control → Rules</span>
              </div>
            </div>
            <div>
              <label style={fieldLabel}>
                MCP Header Mode
                {savedField === 'mcpHeaderMode' && <span style={{ color: 'var(--green)', marginLeft: '6px', fontWeight: 400 }}>Saved</span>}
              </label>
              <select style={selectStyle} value={gw.mcpHeaderMode}
                onChange={e => handleRuntimeSave('mcpHeaderMode', e.target.value)}>
                <option value="passthrough">passthrough — validate + forward as-is</option>
                <option value="generate">generate — create headers for upstream</option>
                <option value="managed">managed — full header control</option>
              </select>
              <div style={{ fontSize: '10px', color: 'var(--muted)', marginTop: '2px' }}>Affects new upstream connections</div>
            </div>
          </div>
        </div>

        {/* Authentication */}
        <div style={{ marginBottom: '24px', paddingTop: '16px', borderTop: '1px dashed var(--border)' }}>
          <div style={sectionTitle}>Authentication</div>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '16px', marginBottom: '6px' }}>
            <div>
              <label style={fieldLabel}>Auth Mode</label>
              <div style={{ fontSize: '13px', color: 'var(--text)', display: 'flex', alignItems: 'center', gap: '6px' }}>
                <span style={{ width: '8px', height: '8px', borderRadius: '50%', background: 'var(--text)' }} />
                {gw.authMode || 'built-in'}
              </div>
            </div>
            <div>
              <label style={fieldLabel}>Secrets</label>
              <div style={{ fontSize: '12px', color: 'var(--muted)' }}>
                Approval secret: {gw.approvalSecretConfigured ? badge('configured', 'var(--green)') : <span style={{ color: 'var(--amber)' }}>not set</span>}
              </div>
              <div style={{ fontSize: '12px', color: 'var(--muted)' }}>
                Slack secret: {gw.slackSecretConfigured ? badge('configured', 'var(--green)') : 'not set'}
              </div>
            </div>
          </div>
          <div>
            {gw.allowedOrigins.length > 0 && (
              <div style={{ fontSize: '12px', color: 'var(--muted)' }}>
                Allowed origins: {gw.allowedOrigins.map(o => <span key={o} style={{ fontFamily: MONO, marginRight: '6px' }}>{o}</span>)}
              </div>
            )}
            {/* Authorization Server details (when external AS) */}
            {gw.authIssuer && (
              <div style={{
                marginTop: '10px', padding: '10px 12px', borderRadius: 'var(--radius)',
                background: 'var(--bg)', border: '1px solid var(--border)', fontSize: '12px',
              }}>
                <div style={{ fontWeight: 600, color: 'var(--text)', marginBottom: '6px' }}>Authorization Server</div>
                <div style={{ display: 'grid', gridTemplateColumns: '120px 1fr', gap: '4px 12px', color: 'var(--muted)' }}>
                  <span>Issuer:</span><span style={{ fontFamily: MONO, color: 'var(--text)' }}>{gw.authIssuer}</span>
                  {gw.authAudience && <><span>Audience:</span><span style={{ fontFamily: MONO, color: 'var(--text)' }}>{gw.authAudience}</span></>}
                  {gw.authJwksUrl && <><span>JWKS URL:</span><span style={{ fontFamily: MONO, color: 'var(--text)', wordBreak: 'break-all' }}>{gw.authJwksUrl}</span></>}
                  {gw.maxDelegationDepth > 0 && <><span>Max delegation:</span><span style={{ color: 'var(--text)' }}>{gw.maxDelegationDepth}</span></>}
                </div>
                <div style={{ fontSize: '10px', color: 'var(--muted)', marginTop: '6px' }}>
                  Configured via <code style={{ fontFamily: MONO }}>auth_server</code> in config file. Requires restart to change.
                </div>
              </div>
            )}
          </div>
        </div>

        {/* Approval Settings */}
        <div style={{ marginBottom: '24px', paddingTop: '16px', borderTop: '1px dashed var(--border)' }}>
          <div style={sectionTitle}>Approval Settings</div>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '16px' }}>
            <div>
              <label style={fieldLabel}>
                Approval TTL{' '}
                {savedField === 'approvalTtl' && <span style={{ color: 'var(--green)', fontWeight: 400 }}>Saved</span>}
              </label>
              <select style={selectStyle} value={gw.approvalTtl || '10m'}
                onChange={e => handleRuntimeSave('approvalTtl', e.target.value)}>
                {['1m', '5m', '10m', '30m', '1h', '4h', '24h'].map(v => (
                  <option key={v} value={v}>{v}</option>
                ))}
              </select>
              <div style={{ fontSize: '10px', color: 'var(--muted)', marginTop: '2px' }}>Time before unanswered approvals expire</div>
            </div>
            <div>
              <label style={fieldLabel}>Approval Storage</label>
              <div style={readOnly}>{badge(gw.approvalStorage || 'memory', 'var(--muted)')}</div>
              <div style={{ fontSize: '10px', color: 'var(--muted)' }}>memory or sqlite. Set via <code style={{ fontFamily: MONO }}>approval_storage</code> in config.</div>
            </div>
          </div>
        </div>

        {/* Notifications */}
        <div style={{ marginBottom: '24px', paddingTop: '16px', borderTop: '1px dashed var(--border)' }}>
          <div style={sectionTitle}>Notifications</div>
          <div style={{ marginBottom: '12px' }}>
            <label style={fieldLabel}>Approval Webhook</label>
            <div style={{ fontSize: '11px', color: 'var(--muted)', marginBottom: '6px' }}>Notified via Slack, Teams, or any webhook when a tool call needs human approval.</div>
            <div style={{ display: 'flex', gap: '6px' }}>
              <input style={{ ...inputStyle, flex: 1 }} value={approvalWebhook} placeholder="https://hooks.slack.com/..."
                onChange={e => setApprovalWebhook(e.target.value)} />
              <button onClick={handleSaveApprovalWebhook} style={{
                padding: '6px 12px', borderRadius: 'var(--radius)', fontSize: '12px',
                background: 'var(--accent)', color: '#fff', border: 'none', cursor: 'pointer',
              }}>Save</button>
              <button onClick={handleTestWebhook} style={{
                padding: '6px 12px', borderRadius: 'var(--radius)', fontSize: '12px',
                background: 'transparent', color: 'var(--muted)', border: '1px solid var(--border)', cursor: 'pointer',
              }}>Test</button>
            </div>
            {webhookStatus && <div style={{ fontSize: '11px', color: webhookStatus.startsWith('✕') ? 'var(--red)' : 'var(--green)', marginTop: '4px' }}>{webhookStatus}</div>}
          </div>
          <div>
            <label style={fieldLabel}>Alert Webhook</label>
            <div style={{ fontSize: '11px', color: 'var(--muted)', marginBottom: '6px' }}>Notified when anomalies are detected (PII exposure, rate spikes, off-hours activity).</div>
            <div style={{ display: 'flex', gap: '6px' }}>
              <input style={{ ...inputStyle, flex: 1 }} value={alertWebhook} placeholder="https://hooks.slack.com/..."
                onChange={e => setAlertWebhook(e.target.value)} />
              <button onClick={handleSaveAlertWebhook} style={{
                padding: '6px 12px', borderRadius: 'var(--radius)', fontSize: '12px',
                background: 'var(--accent)', color: '#fff', border: 'none', cursor: 'pointer',
              }}>Save</button>
            </div>
            {alertWebhookStatus && <div style={{ fontSize: '11px', color: 'var(--green)', marginTop: '4px' }}>{alertWebhookStatus}</div>}
          </div>
        </div>

        {/* Observability */}
        <div style={{ marginBottom: '24px', paddingTop: '16px', borderTop: '1px dashed var(--border)' }}>
          <div style={sectionTitle}>Observability</div>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '16px' }}>
            <div>
              <label style={fieldLabel}>OTel Tracing</label>
              <div style={{ fontSize: '13px', color: 'var(--text)', display: 'flex', alignItems: 'center', gap: '6px' }}>
                <span style={{ width: '8px', height: '8px', borderRadius: '50%', background: gw.tracingEnabled ? 'var(--green)' : 'var(--muted)' }} />
                {gw.tracingEnabled ? 'enabled' : 'disabled'}
                {gw.tracingEnabled && gw.tracingEndpoint && (
                  <span style={{ fontSize: '12px', color: 'var(--muted)', fontFamily: MONO }}>({gw.tracingEndpoint})</span>
                )}
              </div>
              <div style={{ fontSize: '10px', color: 'var(--muted)', marginTop: '2px' }}>
                W3C traceparent from MCP <code style={{ fontFamily: MONO }}>_meta</code>. <a href="#docs" style={{ color: 'var(--accent)' }}>Docs →</a>
              </div>
            </div>
            <div>
              <label style={fieldLabel}>Prometheus Metrics</label>
              <div style={{ fontSize: '13px', color: 'var(--text)', display: 'flex', alignItems: 'center', gap: '6px' }}>
                <span style={{ width: '8px', height: '8px', borderRadius: '50%', background: gw.metricsEnabled ? 'var(--green)' : 'var(--muted)' }} />
                {gw.metricsEnabled ? 'enabled' : 'disabled'}
              </div>
              <div style={{ fontSize: '10px', color: 'var(--muted)', marginTop: '2px' }}>
                Exposed at <code style={{ fontFamily: MONO }}>/metrics</code>. Disable with <code style={{ fontFamily: MONO }}>metrics_enabled: false</code>.
              </div>
            </div>
          </div>
        </div>

        {/* Audit Storage */}
        <div style={{ marginBottom: '24px', paddingTop: '16px', borderTop: '1px dashed var(--border)' }}>
          <div style={sectionTitle}>Audit Storage</div>
          {auditInfo && (
            <div style={{
              display: 'grid', gridTemplateColumns: '80px 1fr', gap: '4px 12px',
              fontSize: '13px', color: 'var(--muted)', marginBottom: '12px',
            }}>
              <span>Storage:</span><span style={{ fontFamily: MONO, color: 'var(--text)' }}>{auditInfo.storage}</span>
              {auditInfo.path && <><span>Path:</span><span style={{ fontFamily: MONO, color: 'var(--text)' }}>{auditInfo.path}</span></>}
              <span>Entries:</span><span style={{ color: 'var(--text)' }}>{auditInfo.entryCount}</span>
              <span>Size:</span><span style={{ color: 'var(--text)' }}>{auditInfo.sizeBytes != null ? `${(auditInfo.sizeBytes / 1024).toFixed(0)} KB` : 'unknown'}</span>
              {auditInfo.oldestEntry && <><span>Oldest:</span><span style={{ color: 'var(--text)' }}>{new Date(auditInfo.oldestEntry).toLocaleString()}</span></>}
              {auditInfo.retention && <><span>Retention:</span><span style={{ color: 'var(--text)' }}>{auditInfo.retention}</span></>}
            </div>
          )}
          <div style={{ fontSize: '11px', color: 'var(--muted)', marginBottom: '12px' }}>
            Entries are automatically purged when they exceed the max entries limit or are older than the retention period (checked hourly).
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
            <span style={{ fontSize: '12px', color: 'var(--muted)' }}>Max entries:</span>
            <input type="number" style={{ ...inputStyle, width: '120px' }} value={maxEntries}
              onChange={e => setMaxEntries(e.target.value)} placeholder="0 = unlimited" />
            <button onClick={handleSaveAuditMax} style={{
              padding: '6px 12px', borderRadius: 'var(--radius)', fontSize: '12px',
              background: 'var(--accent)', color: '#fff', border: 'none', cursor: 'pointer',
            }}>Save</button>
            <button onClick={handlePurge} style={{
              padding: '6px 12px', borderRadius: 'var(--radius)', fontSize: '12px',
              background: 'var(--red)', color: '#fff',
              border: 'none', cursor: 'pointer', fontWeight: 500,
            }}>Purge All</button>
          </div>
        </div>
      </div>

      {/* Right: Connect Agent + Platform */}
      <div style={{ flex: 1, overflow: 'auto', padding: '20px' }}>

        {/* Connect Your Agent */}
        <div style={{ marginBottom: '24px' }}>
          <div style={sectionTitle}>Connect Your Agent</div>
          <div style={{
            padding: '12px 16px', borderRadius: 'var(--radius)',
            border: '1px solid var(--border)', background: 'var(--bg)',
          }}>
            <div style={{ fontSize: '12px', color: 'var(--muted)', marginBottom: '6px' }}>Proxy Endpoint</div>
            <div style={{ fontSize: '14px', fontFamily: MONO, color: 'var(--text)', wordBreak: 'break-all' }}>
              {proxyEndpoint}
            </div>
            <div style={{ fontSize: '11px', color: 'var(--muted)', marginTop: '6px' }}>
              See <a href="#docs" style={{ color: 'var(--accent)' }}>Docs → Framework Guide</a> for connection snippets
            </div>
          </div>
        </div>

        {/* Platform Connection */}
        <div>
          <div style={sectionTitle}>Platform Connection</div>

          {!platformConnected ? (
            <div style={{
              padding: '16px', borderRadius: 'var(--radius)',
              border: '1px solid var(--border)',
            }}>
              <div style={{ fontSize: '12px', color: 'var(--muted)', marginBottom: '12px' }}>Not connected — standalone mode</div>
              {/* Console API fields */}
              <div style={{ fontSize: '11px', fontWeight: 600, color: 'var(--muted)', marginBottom: '8px' }}>Console API (Recall Cloud)</div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: '8px', marginBottom: '16px' }}>
                <div>
                  <label style={fieldLabel}>Console API URL</label>
                  <input style={inputStyle} value={connectUrl} placeholder="https://api.controlis.ai"
                    onChange={e => setConnectUrl(e.target.value)} />
                </div>
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '8px' }}>
                  <div>
                    <label style={fieldLabel}>Service Token</label>
                    <input type="password" style={inputStyle} value={connectToken}
                      onChange={e => setConnectToken(e.target.value)} />
                  </div>
                  <div>
                    <label style={fieldLabel}>Tenant ID</label>
                    <input style={inputStyle} value={connectTenantId}
                      onChange={e => setConnectTenantId(e.target.value)} />
                  </div>
                </div>
                <div>
                  <label style={fieldLabel}>Console UI URL (optional)</label>
                  <input style={inputStyle} value={connectConsoleUrl} placeholder="http://localhost:8083"
                    onChange={e => setConnectConsoleUrl(e.target.value)} />
                </div>
              </div>

              {/* Engine fields */}
              <div style={{ fontSize: '11px', fontWeight: 600, color: 'var(--muted)', marginBottom: '8px' }}>Engine (Your Infrastructure)</div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: '8px', marginBottom: '16px' }}>
                <div>
                  <label style={fieldLabel}>Engine URL</label>
                  <input style={inputStyle} value={connectEngineUrl} placeholder="http://recall-engine:8084"
                    onChange={e => setConnectEngineUrl(e.target.value)} />
                </div>
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '8px' }}>
                  <div>
                    <label style={fieldLabel}>Engine API Key</label>
                    <input type="password" style={inputStyle} value={connectApiKey}
                      onChange={e => setConnectApiKey(e.target.value)} />
                  </div>
                  <div>
                    <label style={fieldLabel}>Default Org ID (optional)</label>
                    <input style={inputStyle} value={connectOrgId}
                      onChange={e => setConnectOrgId(e.target.value)} />
                  </div>
                </div>
              </div>

              {/* Benefits note */}
              <div style={{
                fontSize: '12px', color: 'var(--muted)', padding: '10px 12px',
                background: 'var(--bg)', borderRadius: 'var(--radius)', marginBottom: '12px',
              }}>
                Your local rules are preserved. Platform adds:
                <ul style={{ margin: '4px 0 0 16px', padding: 0 }}>
                  <li>Two-layer evaluation (local rules + engine policies)</li>
                  <li>Per-org governance profiles</li>
                  <li>Compliance scoring (0-1000 EWMA)</li>
                  <li>Audit persistence for regulators</li>
                </ul>
              </div>

              {connectError && <div style={{ fontSize: '12px', color: 'var(--red)', marginBottom: '8px' }}>{connectError}</div>}

              <button onClick={handleConnect} style={{
                padding: '8px 20px', borderRadius: 'var(--radius)', fontSize: '13px', fontWeight: 500,
                background: 'var(--accent)', color: '#fff', border: 'none', cursor: 'pointer',
              }}>
                Connect
              </button>
            </div>
          ) : (
            <div style={{
              padding: '16px', borderRadius: 'var(--radius)',
              border: '1px solid color-mix(in srgb, var(--green) 30%, var(--border))',
            }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '8px' }}>
                <span style={{ color: 'var(--green)' }}>✓</span>
                <span style={{ fontSize: '13px', fontWeight: 600, color: 'var(--text)' }}>Connected to {connectUrl}</span>
              </div>
              {connectResult?.engineConnected && (
                <div style={{ fontSize: '12px', color: 'var(--green)', marginBottom: '4px' }}>
                  Engine connected — two-layer evaluation active
                </div>
              )}
              {connectResult?.engineWarning && (
                <div style={{ fontSize: '12px', color: 'var(--amber)', marginBottom: '4px' }}>
                  ⚠ {connectResult.engineWarning}
                </div>
              )}
              {/* Sync to Platform */}
              {connectResult?.products && connectResult.products.length > 0 && (
                <div style={{ marginTop: '12px', paddingTop: '12px', borderTop: '1px solid var(--border)' }}>
                  <div style={{ fontSize: '11px', fontWeight: 600, color: 'var(--muted)', textTransform: 'uppercase', marginBottom: '6px' }}>
                    Sync to Platform
                  </div>
                  <div style={{ display: 'flex', gap: '6px', alignItems: 'center' }}>
                    <select style={{ ...selectStyle, flex: 1 }} value={syncProductId}
                      onChange={e => setSyncProductId(e.target.value)}>
                      <option value="">Select a product...</option>
                      {connectResult.products.map(p => (
                        <option key={p.id} value={p.id}>{p.name}</option>
                      ))}
                    </select>
                    <button onClick={handleSync} disabled={syncing || !syncProductId} style={{
                      padding: '6px 14px', borderRadius: 'var(--radius)', fontSize: '12px',
                      background: 'var(--accent)', color: '#fff', border: 'none', cursor: 'pointer',
                      opacity: !syncProductId ? 0.5 : 1,
                    }}>
                      {syncing ? 'Syncing...' : 'Sync'}
                    </button>
                  </div>
                  {syncResult && (
                    <div style={{ fontSize: '12px', color: 'var(--green)', marginTop: '6px' }}>{syncResult}</div>
                  )}
                </div>
              )}

              {connectResult?.consoleUrl && (
                <a href={connectResult.consoleUrl} target="_blank" rel="noopener noreferrer" style={{
                  fontSize: '12px', color: 'var(--accent)', display: 'inline-block', marginTop: '8px',
                }}>
                  Open Console →
                </a>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
