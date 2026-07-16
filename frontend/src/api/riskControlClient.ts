import axios from 'axios'

export const riskControlClient = axios.create({
  baseURL: '/risk-control/api/v1',
  withCredentials: true,
  timeout: 15000,
  headers: { 'Content-Type': 'application/json' },
})

riskControlClient.interceptors.request.use((config) => {
  const token = localStorage.getItem('auth_token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  const locale = localStorage.getItem('locale') || 'zh-CN'
  config.headers['Accept-Language'] = locale
  return config
})

riskControlClient.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error?.response?.status === 401 && window.location.pathname.startsWith('/admin/risk-control/')) {
      window.location.href = '/login'
    }
    return Promise.reject(error)
  },
)
