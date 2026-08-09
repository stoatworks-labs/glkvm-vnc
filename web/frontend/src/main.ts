import './styles.css'
import RFB from '@novnc/novnc/core/rfb.js'
import { api, type Endpoint, type EndpointForm } from './api'

const app = document.getElementById('app')!

function h(html: string): HTMLElement {
    const t = document.createElement('template')
    t.innerHTML = html.trim()
    return t.content.firstElementChild as HTMLElement
}

function esc(s: string): string {
    return s.replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c] as string))
}

// ---- router ----

async function route() {
    const hash = location.hash.slice(1)
    const vnc = hash.match(/^\/vnc\/(\d+)$/)
    if (vnc) {
        renderViewer(parseInt(vnc[1], 10))
        return
    }
    const { authenticated } = await api.session()
    if (!authenticated) {
        renderLogin()
    } else {
        renderManagement()
    }
}

window.addEventListener('hashchange', route)
route()

// ---- login ----

function renderLogin() {
    app.innerHTML = ''
    const card = h(`
      <div class="login-wrap"><div class="card login-card">
        <h1 style="margin:0 0 4px">glkvm<span style="color:var(--accent)">-vnc</span></h1>
        <p class="muted" style="margin:0 0 12px">Sign in to manage VNC endpoints</p>
        <label>Password</label>
        <input id="pw" type="password" autocomplete="current-password" />
        <div class="error" id="err"></div>
        <button class="primary" id="go" style="width:100%;margin-top:12px">Sign In</button>
      </div></div>`)
    app.appendChild(card)
    const pw = card.querySelector<HTMLInputElement>('#pw')!
    const err = card.querySelector('#err')!
    const submit = async () => {
        try {
            await api.login(pw.value)
            location.hash = '#/'
            route()
        } catch (e) {
            err.textContent = (e as Error).message
        }
    }
    card.querySelector('#go')!.addEventListener('click', submit)
    pw.addEventListener('keydown', (e) => { if (e.key === 'Enter') submit() })
    pw.focus()
}

// ---- management ----

async function renderManagement() {
    app.innerHTML = ''
    app.appendChild(h(`
      <div class="topbar">
        <div class="brand"><h1>glkvm<span>-vnc</span></h1></div>
        <button id="logout">Log out</button>
      </div>`))
    const container = h(`<div class="container"></div>`)
    app.appendChild(container)

    document.getElementById('logout')!.addEventListener('click', async () => {
        await api.logout(); location.hash = '#/'; route()
    })

    let endpoints: Endpoint[] = []
    let agents: string[] = []
    try {
        endpoints = (await api.listEndpoints()).items
        agents = (await api.listAgents()).items
    } catch (e) {
        container.appendChild(h(`<div class="card error">${esc((e as Error).message)}</div>`))
        return
    }

    const card = h(`<div class="card"></div>`)
    card.appendChild(h(`
      <div class="spread">
        <div style="font-size:16px;font-weight:600">Endpoints (${endpoints.length})</div>
        <button class="primary" id="add">Add endpoint</button>
      </div>`))

    const rows = endpoints.map((e) => `
      <tr>
        <td>${esc(e.name)}</td>
        <td class="muted">${esc(e.addr)}</td>
        <td>${e.agent ? esc(e.agent) : '<span class="muted">direct</span>'}</td>
        <td>${e.hasPassword ? '<span class="lock">🔒 stored</span>' : '<span class="muted">—</span>'}</td>
        <td class="row" style="justify-content:flex-end">
          <button class="primary" data-connect="${e.id}">Connect</button>
          <button data-edit="${e.id}">Edit</button>
          <button class="danger" data-del="${e.id}">Delete</button>
        </td>
      </tr>`).join('')
    card.appendChild(h(`
      <table>
        <thead><tr><th>Name</th><th>Address</th><th>Route</th><th>Password</th><th></th></tr></thead>
        <tbody>${rows || '<tr><td colspan="5" class="muted">No endpoints yet.</td></tr>'}</tbody>
      </table>`))
    container.appendChild(card)

    card.querySelectorAll<HTMLButtonElement>('[data-connect]').forEach((b) =>
        b.addEventListener('click', () => { location.hash = `#/vnc/${b.dataset.connect}` }))
    card.querySelectorAll<HTMLButtonElement>('[data-del]').forEach((b) =>
        b.addEventListener('click', async () => {
            if (!confirm('Delete this endpoint?')) return
            await api.deleteEndpoint(parseInt(b.dataset.del!, 10)); renderManagement()
        }))
    card.querySelectorAll<HTMLButtonElement>('[data-edit]').forEach((b) =>
        b.addEventListener('click', () => showForm(container, agents, endpoints.find((e) => e.id === parseInt(b.dataset.edit!, 10)))))
    card.querySelector('#add')!.addEventListener('click', () => showForm(container, agents))
}

function showForm(container: HTMLElement, agents: string[], editing?: Endpoint) {
    container.querySelector('.form-panel')?.remove()
    const agentOpts = ['<option value="">Direct (dial from gateway)</option>']
        .concat(agents.map((a) => `<option value="${esc(a)}"${editing?.agent === a ? ' selected' : ''}>${esc(a)} (agent)</option>`))
        .join('')
    const panel = h(`
      <div class="card form-panel">
        <div style="font-weight:600;margin-bottom:6px">${editing ? 'Edit endpoint' : 'Add endpoint'}</div>
        <label>Name</label><input id="f-name" value="${esc(editing?.name || '')}" />
        <label>Address (host:port)</label><input id="f-addr" placeholder="192.168.1.50:5900" value="${esc(editing?.addr || '')}" />
        <label>Route</label><select id="f-agent">${agentOpts}</select>
        <div class="hint">Direct requires the gateway to reach the host. An agent tunnels to the agent's LAN (NAT traversal).</div>
        <label>Description</label><input id="f-desc" value="${esc(editing?.description || '')}" />
        <label>VNC password</label><input id="f-pw" type="password" autocomplete="new-password"
          placeholder="${editing?.hasPassword ? 'Leave blank to keep stored password' : 'Optional'}" />
        <div class="hint">Encrypted on the server and injected into the VNC handshake; the browser never sees it.</div>
        ${editing?.hasPassword ? '<label class="row" style="margin-top:10px"><input type="checkbox" id="f-clear" style="width:auto" /> Remove stored password</label>' : ''}
        <div class="error" id="f-err"></div>
        <div class="row" style="margin-top:14px">
          <button class="primary" id="f-save">Save</button>
          <button id="f-cancel">Cancel</button>
        </div>
      </div>`)
    container.appendChild(panel)
    panel.scrollIntoView({ behavior: 'smooth' })

    const val = (id: string) => panel.querySelector<HTMLInputElement>(id)!.value
    const err = panel.querySelector('#f-err')!
    panel.querySelector('#f-cancel')!.addEventListener('click', () => panel.remove())
    panel.querySelector('#f-save')!.addEventListener('click', async () => {
        const data: EndpointForm = {
            name: val('#f-name').trim(),
            addr: val('#f-addr').trim(),
            agent: val('#f-agent'),
            description: val('#f-desc').trim(),
        }
        const clear = panel.querySelector<HTMLInputElement>('#f-clear')?.checked
        const pw = val('#f-pw')
        if (clear) data.password = ''
        else if (pw !== '') data.password = pw
        try {
            if (editing) await api.updateEndpoint(editing.id, data)
            else await api.createEndpoint(data)
            renderManagement()
        } catch (e) {
            err.textContent = (e as Error).message
        }
    })
}

// ---- viewer ----

function renderViewer(id: number) {
    app.innerHTML = ''
    const view = h(`
      <div class="viewer">
        <div class="toolbar">
          <span class="status" id="status" data-s="connecting">Connecting…</span>
          <div class="row">
            <button id="cad">Ctrl+Alt+Del</button>
            <button id="back">Back</button>
          </div>
        </div>
        <div class="screen" id="screen"></div>
      </div>`)
    app.appendChild(view)
    const status = view.querySelector('#status') as HTMLElement
    const screen = view.querySelector('#screen') as HTMLElement

    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const url = `${proto}://${location.host}/connect-vnc/${id}`
    let rfb: any = null

    const connect = () => {
        status.textContent = 'Connecting…'; status.dataset.s = 'connecting'
        rfb = new RFB(screen, url)
        rfb.scaleViewport = true
        rfb.addEventListener('connect', () => { status.textContent = 'Connected'; status.dataset.s = 'connected' })
        rfb.addEventListener('disconnect', (e: any) => {
            status.textContent = e?.detail?.clean ? 'Disconnected' : 'Connection failed'
            status.dataset.s = e?.detail?.clean ? 'disconnected' : 'failed'
            rfb = null
        })
        rfb.addEventListener('credentialsrequired', () => {
            const pw = prompt('VNC password:') || ''
            rfb?.sendCredentials({ password: pw })
        })
    }
    connect()

    view.querySelector('#cad')!.addEventListener('click', () => rfb?.sendCtrlAltDel())
    view.querySelector('#back')!.addEventListener('click', () => {
        try { rfb?.disconnect() } catch { /* already down */ }
        location.hash = '#/'
    })
}
