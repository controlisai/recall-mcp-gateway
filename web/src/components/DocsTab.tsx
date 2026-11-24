import { useState, useRef, useEffect } from 'react'
import { docs } from '@/lib/docs-content'

const sections = [
  { key: 'quickstart', label: 'Quick Start' },
  { key: 'architecture', label: 'Architecture' },
  { key: 'agents', label: 'Agents & Tokens' },
  { key: 'rules', label: 'Rules' },
  { key: 'servers', label: 'MCP Servers' },
  { key: 'approvals', label: 'Approval Flow' },
  { key: 'security', label: 'Security' },
  { key: 'platform', label: 'Platform Connection' },
  { key: 'observability', label: 'Observability' },
  { key: 'api', label: 'API Reference' },
  { key: 'frameworks', label: 'Framework Guide' },
]

export function DocsTab() {
  const [active, setActive] = useState('quickstart')
  const contentRef = useRef<HTMLDivElement>(null)

  // Wire onclick handlers from vanilla HTML (showDoc, switchTab)
  useEffect(() => {
    if (!contentRef.current) return
    contentRef.current.scrollTop = 0

    // Intercept clicks on elements with onclick attributes
    const handler = (e: MouseEvent) => {
      const target = e.target as HTMLElement
      const clickable = target.closest('[onclick]') as HTMLElement | null
      if (!clickable) return
      const onclick = clickable.getAttribute('onclick') || ''
      // Handle showDoc('section')
      const docMatch = onclick.match(/showDoc\(['"](\w+)['"]\)/)
      if (docMatch) { e.preventDefault(); e.stopPropagation(); setActive(docMatch[1]); return }
      // Handle switchTab('tab')
      const tabMatch = onclick.match(/switchTab\(['"](\w+)['"]\)/)
      if (tabMatch) { e.preventDefault(); e.stopPropagation(); window.location.hash = tabMatch[1]; return }
    }
    contentRef.current.addEventListener('click', handler)
    return () => contentRef.current?.removeEventListener('click', handler)
  }, [active])

  const content = docs[active] || '<p style="color:var(--muted)">Section not found</p>'

  return (
    <div style={{ display: 'flex', height: '100%', overflow: 'hidden' }}>
      {/* Left nav */}
      <div style={{
        width: '200px', flexShrink: 0, overflow: 'auto',
        borderRight: '1px solid var(--border)', padding: '16px 0',
      }}>
        <div style={{
          fontSize: '11px', fontWeight: 600, color: 'var(--muted)',
          textTransform: 'uppercase', letterSpacing: '0.05em',
          padding: '0 16px 12px',
        }}>
          Documentation
        </div>
        {sections.map(s => (
          <button key={s.key} onClick={() => setActive(s.key)} style={{
            display: 'block', width: '100%', textAlign: 'left',
            padding: '6px 16px', fontSize: '13px', cursor: 'pointer',
            background: 'transparent', border: 'none', outline: 'none',
            color: active === s.key ? 'var(--accent)' : 'var(--text)',
            fontWeight: active === s.key ? 600 : 400,
            borderLeft: active === s.key ? '2px solid var(--accent)' : '2px solid transparent',
            transition: 'color 0.15s',
          }}>
            {s.label}
          </button>
        ))}
      </div>

      {/* Right content */}
      <div ref={contentRef} style={{ flex: 1, overflow: 'auto', padding: '24px 32px' }}>
        <style>{docsStyles}</style>
        <div dangerouslySetInnerHTML={{ __html: content }} style={{ maxWidth: '800px' }} />
      </div>
    </div>
  )
}

const docsStyles = `
div[style] h2 { font-size: 20px; font-weight: 700; color: var(--text); margin: 0 0 12px; }
div[style] h3 { font-size: 15px; font-weight: 600; color: var(--text); margin: 20px 0 8px; }
div[style] h4 { font-size: 13px; font-weight: 600; color: var(--text); margin: 16px 0 6px; }
div[style] p { font-size: 13px; color: var(--text); line-height: 1.6; margin: 0 0 8px; }
div[style] ul, div[style] ol { font-size: 13px; color: var(--text); line-height: 1.6; margin: 0 0 8px; padding-left: 20px; }
div[style] li { margin-bottom: 4px; }
div[style] code { font-family: 'JetBrains Mono', 'Cascadia Code', 'SF Mono', monospace; font-size: 12px; background: var(--bg); padding: 1px 4px; border-radius: 3px; }
div[style] pre { background: var(--bg); border: 1px solid var(--border); border-radius: var(--radius); padding: 12px 16px; overflow-x: auto; margin: 8px 0 12px; }
div[style] pre code { background: none; padding: 0; font-size: 12px; line-height: 1.5; }
div[style] table { width: 100%; border-collapse: collapse; margin: 8px 0 12px; font-size: 12px; }
div[style] th { text-align: left; padding: 6px 10px; border-bottom: 2px solid var(--border); color: var(--muted); font-weight: 600; font-size: 11px; text-transform: uppercase; letter-spacing: 0.03em; }
div[style] td { padding: 6px 10px; border-bottom: 1px solid var(--border); color: var(--text); vertical-align: top; }
div[style] a { color: var(--accent); }
div[style] svg { max-width: 100%; height: auto; }
.docs-content h2 { font-size: 20px; font-weight: 700; color: var(--text); margin: 0 0 12px; }
.docs-content h3 { font-size: 15px; font-weight: 600; color: var(--text); margin: 20px 0 8px; }
.docs-content h4 { font-size: 13px; font-weight: 600; color: var(--text); margin: 16px 0 6px; }
.docs-content p { font-size: 13px; color: var(--text); line-height: 1.6; margin: 0 0 8px; }
.docs-content ul, .docs-content ol { font-size: 13px; color: var(--text); line-height: 1.6; margin: 0 0 8px; padding-left: 20px; }
.docs-content li { margin-bottom: 4px; }
.docs-content code { font-family: 'JetBrains Mono', 'Cascadia Code', 'SF Mono', monospace; font-size: 12px; background: var(--bg); padding: 1px 4px; border-radius: 3px; }
.docs-content pre { background: var(--bg); border: 1px solid var(--border); border-radius: var(--radius); padding: 12px 16px; overflow-x: auto; margin: 8px 0 12px; }
.docs-content pre code { background: none; padding: 0; font-size: 12px; line-height: 1.5; }
.docs-content table { width: 100%; border-collapse: collapse; margin: 8px 0 12px; font-size: 12px; }
.docs-content th { text-align: left; padding: 6px 10px; border-bottom: 2px solid var(--border); color: var(--muted); font-weight: 600; font-size: 11px; text-transform: uppercase; letter-spacing: 0.03em; }
.docs-content td { padding: 6px 10px; border-bottom: 1px solid var(--border); color: var(--text); vertical-align: top; }
.docs-content a { color: var(--accent); }
.docs-content svg { max-width: 100%; height: auto; }
`
