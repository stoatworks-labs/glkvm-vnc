export interface Endpoint {
    id: number
    name: string
    addr: string
    agent: string
    description: string
    hasPassword: boolean
    createdAt: number
    updatedAt: number
}

export interface EndpointForm {
    name: string
    addr: string
    agent: string
    description: string
    password?: string
}

async function req<T>(url: string, opts: RequestInit = {}): Promise<T> {
    const res = await fetch(url, {
        ...opts,
        headers: { 'Content-Type': 'application/json', ...(opts.headers || {}) },
    })
    const body = await res.json().catch(() => ({}))
    if (!res.ok) {
        throw new Error(body.error || `HTTP ${res.status}`)
    }
    return body as T
}

export const api = {
    session: () => req<{ authenticated: boolean }>('/api/session'),
    login: (password: string) => req('/api/login', { method: 'POST', body: JSON.stringify({ password }) }),
    logout: () => req('/api/logout', { method: 'POST' }),
    listEndpoints: () => req<{ items: Endpoint[] }>('/api/endpoints'),
    listAgents: () => req<{ items: string[] }>('/api/agents'),
    createEndpoint: (data: EndpointForm) => req<{ id: number }>('/api/endpoints', { method: 'POST', body: JSON.stringify(data) }),
    updateEndpoint: (id: number, data: EndpointForm) => req<{ id: number }>(`/api/endpoints/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
    deleteEndpoint: (id: number) => req(`/api/endpoints/${id}`, { method: 'DELETE' }),
}
